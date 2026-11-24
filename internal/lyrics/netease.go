package lyrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 网易云音乐作为第二歌词源。
//
// 为什么需要它：LRCLIB 覆盖面偏欧美 + 主流中文歌，网络歌手、翻唱、民乐/纯音乐
// 的命中率明显偏低（实测本地 632 首里一大半 LRCLIB 根本没有记录）。
// 网易云的公开搜索+歌词接口不需要登录，且中文曲库最全，拿来做兜底刚好。
//
// 注意：这只是「读」接口（搜索 + 取歌词），不使用用户账号，不涉及播放/下载。

// coolNetease 让所有 worker 在接下来 30 秒内跳过网易云。
func (c *Client) coolNetease() {
	c.netMu.Lock()
	c.netCool = time.Now().Add(30 * time.Second)
	c.netMu.Unlock()
}

const (
	neteaseSearchPath = "/api/search/get/web"
	neteaseLyricPath  = "/api/song/lyric"
	neteaseUA         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	neteaseReferer    = "https://music.163.com/"
)

// NeteaseBase 是网易云接口的根地址（测试时可指向本地假服务）。
func (c *Client) neteaseBase() string {
	if c.NeteaseBase != "" {
		return strings.TrimRight(c.NeteaseBase, "/")
	}
	return "https://music.163.com"
}

type neteaseSong struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Duration int64  `json:"duration"` // 毫秒
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
}

type neteaseSearchResp struct {
	Result struct {
		Songs []neteaseSong `json:"songs"`
	} `json:"result"`
	Code int `json:"code"`
}

type neteaseLyricResp struct {
	Lrc struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
	PureMusic bool `json:"pureMusic"`
	Code      int  `json:"code"`
}

// errNeteaseCooldown 表示网易云正在限流冷却中，这一轮先别打它。
var errNeteaseCooldown = fmt.Errorf("netease: 冷却中")

func (c *Client) neteaseGet(ctx context.Context, rawURL string, out any) error {
	// 网易云对匿名请求的频控比 LRCLIB 严（返回 405「操作频繁」），
	// 所以单独一套节流 + 冷却：被限流后全局停 30 秒，别让并发的 worker 继续撞。
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if traceOn() {
		defer func(t time.Time) {
			tracef("netease %s", time.Since(t).Round(time.Millisecond))
		}(time.Now())
	}

	c.netMu.Lock()
	if time.Now().Before(c.netCool) {
		c.netMu.Unlock()
		return errNeteaseCooldown
	}
	if d := time.Since(c.netLast); d < 1200*time.Millisecond {
		select {
		case <-time.After(1200*time.Millisecond - d):
		case <-ctx.Done():
			c.netMu.Unlock()
			return ctx.Err()
		}
	}
	c.netLast = time.Now()
	c.netMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", neteaseUA)
	req.Header.Set("Referer", neteaseReferer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		c.coolNetease()
		return fmt.Errorf("netease: HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("netease: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if bytes.Contains(body, []byte(`"code":405`)) || bytes.Contains(body, []byte(`操作频繁`)) {
		c.coolNetease()
		return fmt.Errorf("netease: 被限流（405）")
	}
	return json.Unmarshal(body, out)
}

// neteaseSearch 用「歌名 + 艺人」去网易云搜歌，返回可用作比较的记录（还没有歌词）。
func (c *Client) neteaseSearch(ctx context.Context, q string, limit int) ([]Record, error) {
	if !c.Netease {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	v := url.Values{}
	v.Set("s", q)
	v.Set("type", "1")
	v.Set("limit", strconv.Itoa(limit))
	v.Set("offset", "0")
	var out neteaseSearchResp
	if err := c.neteaseGet(ctx, c.neteaseBase()+neteaseSearchPath+"?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	var recs []Record
	for _, s := range out.Result.Songs {
		if s.ID == 0 || strings.TrimSpace(s.Name) == "" {
			continue
		}
		var names []string
		for _, a := range s.Artists {
			if t := strings.TrimSpace(a.Name); t != "" {
				names = append(names, t)
			}
		}
		recs = append(recs, Record{
			ID:         s.ID,
			TrackName:  strings.TrimSpace(s.Name),
			ArtistName: strings.Join(names, " / "),
			AlbumName:  strings.TrimSpace(s.Album.Name),
			Duration:   float64(s.Duration) / 1000.0,
			Source:     "netease",
		})
	}
	return recs, nil
}

// neteaseLyric 取某一首歌的歌词（LRC 优先，纯文本兜底）。
func (c *Client) neteaseLyric(ctx context.Context, id int64) (Record, error) {
	v := url.Values{}
	v.Set("id", strconv.FormatInt(id, 10))
	v.Set("lv", "1")
	v.Set("kv", "1")
	v.Set("tv", "-1")
	var out neteaseLyricResp
	if err := c.neteaseGet(ctx, c.neteaseBase()+neteaseLyricPath+"?"+v.Encode(), &out); err != nil {
		return Record{}, err
	}
	lrc := strings.TrimSpace(out.Lrc.Lyric)
	if lrc == "" || out.PureMusic {
		return Record{}, ErrNotFound
	}
	rec := Record{ID: id, SyncedLyrics: lrc, Source: "netease"}
	if d := ParseLRC(lrc); d != nil && len(d.Cues) > 0 {
		var lines []string
		for _, cue := range d.Cues {
			if t := strings.TrimSpace(cue.Text); t != "" {
				lines = append(lines, t)
			}
		}
		rec.PlainLyrics = strings.Join(lines, "\n")
	} else {
		// 没有时间轴的（少数）当纯文本用
		rec.SyncedLyrics = ""
		rec.PlainLyrics = lrc
	}
	return rec, nil
}

// bestNetease 在 LRCLIB 无结果时用网易云兜底：同样先过「歌名 + 艺人」双门槛，
// 选中之后再取一次歌词。返回的 Match 里 Record.Source = "netease"。
func (c *Client) bestNetease(ctx context.Context, variants []Variant, album string, duration float64) *Match {
	if !c.Netease {
		return nil
	}
	seen := map[int64]bool{}
	var chosen *Match
	queries := 0
	for _, v := range variants {
		title := strings.TrimSpace(v.Title)
		if title == "" || len([]rune(title)) < 2 || len([]rune(title)) > 30 {
			continue
		}
		if queries >= 2 { // 最多搜两组写法，控制请求数
			break
		}
		queries++
		q := title
		if a := strings.TrimSpace(v.Artist); a != "" {
			q = title + " " + a
		}
		recs, err := c.neteaseSearch(ctx, q, 10)
		if err != nil {
			continue
		}
		for _, r := range recs {
			if r.ID != 0 {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
			}
			tSim := similarity(title, r.TrackName)
			aSim := 0.0
			if v.Artist != "" {
				aSim = artistSimilarity(v.Artist, r.ArtistName)
			}
			if tSim < minTitleSim || (v.Artist != "" && aSim < minArtistSim) {
				continue
			}
			if noisyName(r.TrackName) || noisyName(r.ArtistName) {
				continue
			}
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
					continue // 时长差得离谱，多半是同名不同歌
				}
			}
			if chosen == nil || score > chosen.Score {
				chosen = &Match{Record: r, Variant: v, TitleSim: tSim, ArtistSim: aSim, Score: score}
			}
		}
		// 已经找到「歌名 + 艺人都很硬」的，别再翻后面的写法
		if chosen != nil && chosen.Score >= 150 {
			break
		}
	}
	// 组合查询没结果时，用歌名单独再搜一次（"003L 桃籽" 这种艺名写法在网易云叫 "L（桃籽）"）
	if chosen == nil {
		for _, v := range variants {
			title := strings.TrimSpace(v.Title)
			if title == "" || len([]rune(title)) < 2 || len([]rune(title)) > 30 {
				continue
			}
			recs, err := c.neteaseSearch(ctx, title, 10)
			if err != nil {
				break
			}
			for _, r := range recs {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				tSim := similarity(title, r.TrackName)
				aSim := artistSimilarity(v.Artist, r.ArtistName)
				if tSim < 0.9 || aSim < 0.45 {
					continue
				}
				if duration > 0 && r.Duration > 0 {
					d := duration - r.Duration
					if d < 0 {
						d = -d
					}
					if d > 25 {
						continue
					}
				}
				score := tSim*100 + aSim*60 - 20
				if chosen == nil || score > chosen.Score {
					chosen = &Match{Record: r, Variant: v, TitleSim: tSim, ArtistSim: aSim, Score: score}
				}
			}
			if chosen != nil {
				break
			}
		}
	}
	if chosen == nil {
		return nil
	}
	// 取歌词；拿到后再把标题/艺人/专辑/时长补全，方便回填曲库
	rec, err := c.neteaseLyric(ctx, chosen.Record.ID)
	if err != nil {
		return nil
	}
	rec.TrackName = chosen.Record.TrackName
	rec.ArtistName = chosen.Record.ArtistName
	rec.AlbumName = chosen.Record.AlbumName
	rec.Duration = chosen.Record.Duration
	chosen.Record = rec
	return chosen
}
