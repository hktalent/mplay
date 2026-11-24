package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 酷狗音乐作为第二歌词源。
//
// 为什么不只用 LRCLIB：LRCLIB 覆盖面偏欧美 + 主流华语，网络歌手、翻唱、民乐
// 这类基本查不到（本地 632 首实测里一大半 LRCLIB 没有记录）。酷狗的公开搜索 +
// 歌词下载接口不需要登录、频控宽松，正好补上华语短板。
//
// 只做「搜索 + 取歌词」两个只读请求，不涉及播放地址、不涉及账号。

const (
	kugouSearchPath = "/song_search_v2"
	kugouLyricFind  = "/search"
	kugouLyricDown  = "/download"
	kugouUA         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	kugouRateWindow = 900 * time.Millisecond
)

// flexInt 兼容酷狗接口里「有时候是数字有时候是字符串」的字段。
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	// 可能是 "166" 也可能是 166.0
	if dot := strings.IndexByte(s, '.'); dot > 0 {
		s = s[:dot]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*f = 0
		return nil // 解析不了就当 0，别让整条记录丢掉
	}
	*f = flexInt(n)
	return nil
}

type kugouSong struct {
	SongName   string  `json:"SongName"`
	SingerName string  `json:"SingerName"`
	FileHash   string  `json:"FileHash"`
	AlbumName  string  `json:"AlbumName"`
	Duration   flexInt `json:"Duration"` // 秒
	AlbumID    flexInt `json:"AlbumID"`
}

type kugouSearchResp struct {
	Status int `json:"status"`
	Data   struct {
		Total int         `json:"total"`
		Lists []kugouSong `json:"lists"`
	} `json:"data"`
	ErrorMsg string `json:"error_msg"`
}

type kugouCandidate struct {
	ID        string `json:"id"`
	AccessKey string `json:"accesskey"`
	Song      string `json:"song"`
	Singer    string `json:"singer"`
	Duration  int64  `json:"duration"`
}

type kugouLyricFindResp struct {
	Status     int              `json:"status"`
	ErrorCode  int              `json:"errcode"`
	Candidates []kugouCandidate `json:"candidates"`
}

type kugouLyricDownResp struct {
	Status  int    `json:"status"`
	Content string `json:"content"` // base64 的 LRC
	Fmt     string `json:"fmt"`
}

// kugouBase 是酷狗接口根地址（测试时指向假服务）。
func (c *Client) kugouBase() string {
	if c.KugouBase != "" {
		return strings.TrimRight(c.KugouBase, "/")
	}
	return "https://songsearch.kugou.com"
}

// kugouLyricFindBase / kugouLyricBase 默认走官方域名，测试时可覆盖。
func (c *Client) kugouLyricFindBase() string {
	if c.KugouBase != "" {
		return strings.TrimRight(c.KugouBase, "/")
	}
	return "https://krcs.kugou.com"
}

func (c *Client) kugouLyricBase() string {
	if c.KugouBase != "" {
		return strings.TrimRight(c.KugouBase, "/")
	}
	return "https://lyrics.kugou.com"
}

func (c *Client) kugouGet(ctx context.Context, rawURL string, out any) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if traceOn() {
		defer func(t time.Time) { tracef("kugou %v", time.Since(t).Round(time.Millisecond)) }(time.Now())
	}
	// 酷狗频控比 LRCLIB 松，但也别并发猛冲
	c.kgMu.Lock()
	if d := time.Since(c.kgLast); d < kugouRateWindow {
		time.Sleep(kugouRateWindow - d)
	}
	c.kgLast = time.Now()
	c.kgMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", kugouUA)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kugou: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if bytesErr := json.Unmarshal(body, out); bytesErr != nil {
		return bytesErr
	}
	return nil
}

// KugouSearchProbe 仅供本地排查用（cmd/probe 里打印候选）。
func (c *Client) KugouSearchProbe(ctx context.Context, keyword string, pageSize int) ([]Record, error) {
	return c.kugouSearch(ctx, keyword, pageSize)
}

// kugouSearch 按关键词搜歌（返回的记录还没歌词）。
func (c *Client) kugouSearch(ctx context.Context, keyword string, pageSize int) ([]Record, error) {
	if !c.Kugou {
		return nil, ErrNotFound
	}
	if pageSize <= 0 || pageSize > 20 {
		pageSize = 10
	}
	v := url.Values{}
	v.Set("keyword", keyword)
	v.Set("page", "1")
	v.Set("pagesize", strconv.Itoa(pageSize))
	v.Set("platform", "WebFilter")
	v.Set("tag", "em")
	v.Set("filter", "2")
	v.Set("iscorrection", "1")
	v.Set("privilege_filter", "0")
	var out kugouSearchResp
	if err := c.kugouGet(ctx, c.kugouBase()+kugouSearchPath+"?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	var recs []Record
	for _, s := range out.Data.Lists {
		name := strings.TrimSpace(kugouHTMLRe.ReplaceAllString(s.SongName, ""))
		singer := strings.TrimSpace(kugouHTMLRe.ReplaceAllString(s.SingerName, ""))
		albumName := strings.TrimSpace(kugouHTMLRe.ReplaceAllString(s.AlbumName, ""))
		if name == "" || s.FileHash == "" {
			continue
		}
		recs = append(recs, Record{
			ID:         0, // 酷狗用 hash 标识，歌词下载另走一次搜索
			TrackName:  name,
			ArtistName: singer,
			AlbumName:  albumName,
			Duration:   float64(int64(s.Duration)),
			Source:     "kugou",
			Hash:       s.FileHash,
		})
	}
	return recs, nil
}

// KRC/LRC 头部的元信息（[ti:]/[total:]/[qq:]…）都剥掉，只留正文；
// 时间标签是数字开头，不会被这条规则误伤。
var krcTagRe = regexp.MustCompile(`(?m)^\[[a-zA-Z]{1,10}:[^\]]*\]\s*$`)

// 酷狗搜索结果里的歌名/歌手带高亮标签（"<em>晚风作酒</em>"），不剥掉相似度会直接崩。
var kugouHTMLRe = regexp.MustCompile(`<[^>]+>`)

// kugouLyric 取一首歌的 LRC（先按 hash 找歌词候选，再下载第一条）。
func (c *Client) kugouLyric(ctx context.Context, rec Record) (Record, error) {
	v := url.Values{}
	v.Set("ver", "1")
	v.Set("man", "yes")
	v.Set("client", "mobi")
	v.Set("keyword", rec.TrackName)
	v.Set("hash", rec.Hash)
	if rec.Duration > 0 {
		v.Set("duration", strconv.FormatInt(int64(rec.Duration), 10))
	}
	var finds kugouLyricFindResp
	if err := c.kugouGet(ctx, c.kugouLyricFindBase()+kugouLyricFind+"?"+v.Encode(), &finds); err != nil {
		return Record{}, err
	}
	if len(finds.Candidates) == 0 {
		return Record{}, ErrNotFound
	}
	cand := finds.Candidates[0]
	if cand.ID == "" || cand.AccessKey == "" {
		return Record{}, ErrNotFound
	}
	v2 := url.Values{}
	v2.Set("ver", "1")
	v2.Set("client", "pc")
	v2.Set("id", cand.ID)
	v2.Set("accesskey", cand.AccessKey)
	v2.Set("fmt", "lrc")
	v2.Set("charset", "utf8")
	var down kugouLyricDownResp
	if err := c.kugouGet(ctx, c.kugouLyricBase()+kugouLyricDown+"?"+v2.Encode(), &down); err != nil {
		return Record{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(down.Content))
	if err != nil || len(raw) == 0 {
		return Record{}, ErrNotFound
	}
	text := strings.TrimPrefix(strings.TrimSpace(string(raw)), "\ufeff")
	text = krcTagRe.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return Record{}, ErrNotFound
	}
	out := rec
	out.Source = "kugou"
	// 纯音乐在酷狗这边也会返回一段“歌词”，内容是「纯音乐，请欣赏」这种占位：
	// 把它当命中会在界面上出现一行假歌词，所以单独标记出来。
	if isInstrumentalText(text) {
		out.Instrumental = true
		return out, nil
	}
	if d := ParseLRC(text); d != nil && len(d.Cues) > 0 {
		out.SyncedLyrics = text
		var lines []string
		for _, cue := range d.Cues {
			if t := strings.TrimSpace(cue.Text); t != "" {
				lines = append(lines, t)
			}
		}
		out.PlainLyrics = strings.Join(lines, "\n")
	} else {
		out.PlainLyrics = text
	}
	return out, nil
}

// bestKugou 是 LRCLIB 之后的第二顺位：同样先过「歌名 + 艺人」双门槛，
// 选中之后才去取歌词（歌词接口两次请求，比较贵）。
func (c *Client) bestKugou(ctx context.Context, variants []Variant, album string, duration float64) *Match {
	if !c.Kugou {
		return nil
	}
	var chosen *Match
	seen := map[string]bool{}
	queries := 0
	try := func(v Variant, keyword string, minTitle, minArtist float64) {
		recs, err := c.kugouSearch(ctx, keyword, 10)
		if err != nil {
			return
		}
		for _, r := range recs {
			k := r.Hash
			if seen[k] {
				continue
			}
			// 酷狗的歌名常带版本后缀（"相思 (Live)"、"潇湘水云 (古琴名曲)"），
			// 直接比会把同一个歌名算成不同歌，所以按「去掉括号后缀的核心名」比。
			tSim := similarity(coreTitle(v.Title), coreTitle(r.TrackName))
			aSim := artistSimilarity(v.Artist, r.ArtistName)
			if tSim < minTitle || (v.Artist != "" && aSim < minArtist) {
				continue
			}
			if noisyName(r.TrackName) || noisyName(r.ArtistName) {
				continue
			}
			// 这条在某个写法下不合格，不代表在另一个写法下也不合格（歌名/艺人反序时
			// 相似度完全不同），所以只在真正采纳之后才标记「见过」。
			seen[k] = true
			score := tSim*100 + aSim*60
			if album != "" {
				score += similarity(album, r.AlbumName) * 12
			}
			if duration > 0 && r.Duration > 0 {
				d := duration - r.Duration
				if d < 0 {
					d = -d
				}
				switch {
				case d <= 3:
					score += 12
				case d <= 8:
					score += 6
				case d > 25:
					if aSim < 0.85 { // 艺人不够硬 + 时长差太多 → 多半同名不同歌
						continue
					}
					score -= 10 // DJ 版/加长版：艺人对得上就只扣分
				}
			}
			if chosen == nil || score > chosen.Score {
				chosen = &Match{Record: r, Variant: v, TitleSim: tSim, ArtistSim: aSim, Score: score}
			}
		}
	}

	for _, v := range variants {
		title := strings.TrimSpace(v.Title)
		if title == "" || len([]rune(title)) < 2 || len([]rune(title)) > 30 {
			continue
		}
		if queries >= 2 {
			break
		}
		queries++
		kw := title
		if a := strings.TrimSpace(v.Artist); a != "" {
			kw = title + " " + a
		}
		try(v, kw, minTitleSim, minArtistSim)
		if chosen != nil && chosen.Score >= 150 {
			break
		}
	}
	// 组合关键词没结果时，歌名单独再搜一次（艺名写法差异很常见）
	if chosen == nil {
		for _, v := range variants {
			title := strings.TrimSpace(v.Title)
			if title == "" || len([]rune(title)) < 2 || len([]rune(title)) > 30 {
				continue
			}
			try(v, title, 0.9, 0.45)
			if chosen != nil {
				break
			}
		}
	}
	if chosen == nil {
		return nil
	}
	rec, err := c.kugouLyric(ctx, chosen.Record)
	if err != nil {
		return nil
	}
	chosen.Record = rec
	chosen.Instrumental = rec.Instrumental
	return chosen
}
