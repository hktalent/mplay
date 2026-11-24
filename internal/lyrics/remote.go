package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// traceOn 打开后（MPLAY_TRACE=1）把每一步的耗时打到 stderr，用于排查慢点。
func traceOn() bool { return os.Getenv("MPLAY_TRACE") == "1" }

func tracef(format string, args ...any) {
	if traceOn() {
		fmt.Fprintf(os.Stderr, "[mplay] "+format+"\n", args...)
	}
}

// ErrNotFound 表示远端确实没有可用歌词（不是网络错误）。
var ErrNotFound = errors.New("未找到匹配的歌词")

// Record 是 LRCLIB 的一条歌词记录。LRCLIB 是一个免费、无需密钥的公共歌词库，
// 同时提供纯文本与带时间轴的 LRC，正好满足「按文件名自动拉歌词」。
type Record struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`

	// Source 记录这条记录来自哪个源：lrclib / kugou / netease。空值按 lrclib 处理。
	Source string `json:"-"`
	// Hash 是酷狗的文件 hash（取歌词要用），别的源为空。
	Hash string `json:"-"`

	Score float64 `json:"-"`
}

// HasSynced 判断这条记录是否带时间轴。
func (r Record) HasSynced() bool { return strings.TrimSpace(r.SyncedLyrics) != "" }

// Client 封装 LRCLIB + 本地歌词缓存。
type Client struct {
	Base     string
	CacheDir string
	Enabled  bool
	// Netease 打开后，LRCLIB 没结果时会用网易云兜底（中文曲库更全）。
	Netease bool
	// NeteaseBase 一般留空（走官方域名），测试时指向假服务。
	NeteaseBase string
	// Kugou 打开后，LRCLIB 没结果时用酷狗兜底（华语曲库最全）。
	Kugou bool
	// KugouBase 一般留空（走官方域名），测试时指向假服务。
	KugouBase string
	kgMu      sync.Mutex
	kgLast    time.Time
	HTTP      *http.Client

	mu   sync.Mutex
	last time.Time
	// sem 限制同时在飞的请求数——多 worker + 多候选并发时，
	// 不加限制会被歌词库判成洪水（LRCLIB 429 / 网易云 405）。
	sem chan struct{}

	// 网易云有自己的节流与冷却（频控比 LRCLIB 严）
	netMu   sync.Mutex
	netLast time.Time
	netCool time.Time
}

// NewClient 创建客户端。base 为空时用官方地址。
func NewClient(base, cacheDir string, enabled bool) *Client {
	if base == "" {
		base = "https://lrclib.net"
	}
	return &Client{
		sem:      make(chan struct{}, 6),
		Base:     strings.TrimRight(base, "/"),
		CacheDir: cacheDir,
		Enabled:  enabled,
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          20,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 8 * time.Second,
			},
		},
	}
}

func (c *Client) acquire(ctx context.Context) error {
	if c.sem == nil {
		return nil
	}
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() {
	if c.sem != nil {
		<-c.sem
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if traceOn() {
		defer func(t time.Time) {
			tracef("lrclib %s %v", q.Encode(), time.Since(t).Round(time.Millisecond))
		}(time.Now())
	}
	// LRCLIB 有人肉限流约 1 请求/秒；这里做一点客户端侧节流，避免批量补歌词被封。
	c.throttle()
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mplay/1.0 (https://m.51pwn.com)")
	req.Header.Set("Accept", "application/json")
	for attempt := 0; ; attempt++ {
		throttleBetween(c, attempt)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		}
		// LRCLIB 会按 IP 限流（429 / 503）。退避一次再试，别把整批缓存的节奏带崩。
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < 3 {
			wait := 1500 * time.Millisecond * time.Duration(attempt+1)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if sec, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && sec > 0 && sec <= 10 {
					wait = time.Duration(sec) * time.Second
				}
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("lrclib %s: %s %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
}

// throttleBetween 在重试时多等一会儿（第一次就只用基础节流）。
func throttleBetween(c *Client, attempt int) {
	if attempt > 0 {
		return // 重试路径已经单独 sleep
	}
	c.throttle()
}

func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// LRCLIB 对匿名请求差不多是 1 次/秒的量级，这里留 600ms 间隔；
	// 撞上 429 时由上面 get() 的退避重试兜住。
	if d := time.Since(c.last); d < 600*time.Millisecond {
		time.Sleep(600*time.Millisecond - d)
	}
	c.last = time.Now()
}

// SearchTrack 按「歌名 + 艺人」查。
func (c *Client) SearchTrack(ctx context.Context, track, artist string) ([]Record, error) {
	if !c.Enabled {
		return nil, ErrNotFound
	}
	q := url.Values{}
	if track != "" {
		q.Set("track_name", track)
	}
	if artist != "" {
		q.Set("artist_name", artist)
	}
	if track == "" && artist == "" {
		return nil, ErrNotFound
	}
	var recs []Record
	if err := c.get(ctx, "/api/search", q, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// SearchQuery 是自由文本搜索；LRCLIB 的 q 会连歌词正文一起搜，
// 因此「输入一句歌词就能找到歌」这个需求可以直接用它实现。
func (c *Client) SearchQuery(ctx context.Context, q string) ([]Record, error) {
	if !c.Enabled {
		return nil, ErrNotFound
	}
	vals := url.Values{}
	vals.Set("q", q)
	var recs []Record
	if err := c.get(ctx, "/api/search", vals, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// GetExact 走 /api/get（要求时长接近），命中率比 search 高但参数要求严。
func (c *Client) GetExact(ctx context.Context, track, artist, album string, duration float64) (*Record, error) {
	if !c.Enabled {
		return nil, ErrNotFound
	}
	q := url.Values{}
	q.Set("track_name", track)
	q.Set("artist_name", artist)
	if album != "" {
		q.Set("album_name", album)
	}
	if duration > 0 {
		q.Set("duration", strconv.Itoa(int(duration+0.5)))
	}
	var rec Record
	if err := c.get(ctx, "/api/get", q, &rec); err != nil {
		return nil, err
	}
	if rec.TrackName == "" {
		return nil, ErrNotFound
	}
	return &rec, nil
}

// Candidates 用多种查询组合取候选，并按相似度打分排序。
// 用 search 而不是 get：文件名的艺人/专辑写法常常和歌词库不一致，search 更宽容。
func (c *Client) Candidates(ctx context.Context, title, artist, album string, duration float64) ([]Record, error) {
	if !c.Enabled {
		return nil, ErrNotFound
	}
	seen := map[int64]bool{}
	var all []Record
	add := func(recs []Record) {
		for _, r := range recs {
			if r.ID != 0 && seen[r.ID] {
				continue
			}
			if r.ID != 0 {
				seen[r.ID] = true
			}
			if strings.TrimSpace(r.TrackName) == "" {
				continue
			}
			r.Score = scoreMatch(title, artist, album, duration, r)
			all = append(all, r)
		}
	}

	if recs, err := c.SearchTrack(ctx, title, artist); err == nil {
		add(recs)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if len(all) < 3 {
		if recs, err := c.SearchQuery(ctx, strings.TrimSpace(title+" "+artist)); err == nil {
			add(recs)
		}
	}
	if len(all) < 3 && title != "" {
		if recs, err := c.SearchQuery(ctx, title); err == nil {
			add(recs)
		}
	}
	if len(all) < 2 && duration > 0 && album != "" {
		if rec, err := c.GetExact(ctx, title, artist, album, duration); err == nil {
			rec.Score = scoreMatch(title, artist, album, duration, *rec) + 10
			add([]Record{*rec})
		}
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > 8 {
		all = all[:8]
	}
	return all, nil
}

// Resolve 是「给一首本地歌找到可播放歌词」的主入口：
// 缓存 → 候选打分 → 取最高分 → 写缓存。force=true 时忽略缓存。
// pick>0 时直接采用指定 LRCLIB ID（给 UI 手动选歌词用）。
func (c *Client) Resolve(ctx context.Context, key, title, artist, album string, duration float64, force bool, pick int64) (*Doc, error) {
	if !force && pick == 0 {
		if doc, err := c.ReadCache(key); err == nil {
			doc.Source = "cache"
			return doc, nil
		}
	}
	if !c.Enabled {
		return nil, ErrNotFound
	}

	cands, err := c.Candidates(ctx, title, artist, album, duration)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, ErrNotFound
	}

	var chosen *Record
	if pick > 0 {
		for i := range cands {
			if cands[i].ID == pick {
				chosen = &cands[i]
				break
			}
		}
		if chosen == nil {
			// 指定的 ID 不在候选里：单独取一条（LRCLIB 没有 /api/get/id，退化为按名字搜到的内容）
			chosen = &cands[0]
		}
	} else {
		best := cands[0]
		// 分数门槛：标题必须像，且不能只是「同一艺人另一首歌」
		if best.Score < 60 {
			return nil, ErrNotFound
		}
		chosen = &best
	}

	doc := DocFromRecord(*chosen)
	doc.Source = "lrclib"
	if err := c.WriteCache(key, doc); err != nil {
		debugLog("写歌词缓存失败: %v", err)
	}
	return doc, nil
}

// DocFromRecord 把记录转成统一 cue 模型。带时间轴优先；只有纯文本时也返回，让用户至少能看到字。
func DocFromRecord(r Record) *Doc {
	src := r.Source
	if src == "" {
		src = "lrclib"
	}
	doc := &Doc{
		Title:    r.TrackName,
		Artist:   r.ArtistName,
		Album:    r.AlbumName,
		Duration: r.Duration,
		LRCLIBID: r.ID,
		Meta:     map[string]string{src: "1"},
	}
	if r.HasSynced() {
		if d := ParseLRC(r.SyncedLyrics); d != nil && len(d.Cues) > 0 {
			d.Title, d.Artist, d.Album = r.TrackName, r.ArtistName, r.AlbumName
			d.Duration = r.Duration
			d.LRCLIBID = r.ID
			d.Source = src
			d.Format = "lrc"
			d.Synced = true
			return d
		}
	}
	if strings.TrimSpace(r.PlainLyrics) != "" {
		d := ParsePlain(r.PlainLyrics)
		d.Title, d.Artist, d.Album = r.TrackName, r.ArtistName, r.AlbumName
		d.Duration = r.Duration
		d.LRCLIBID = r.ID
		d.Source = src
		d.Format = "txt"
		d.Synced = false
		return d
	}
	doc.Source = src
	doc.Format = "txt"
	doc.Synced = false
	return doc
}

// CachePath 是某首歌的歌词缓存文件（按媒体 ID 命名）。
func (c *Client) CachePath(key string) string {
	return filepath.Join(c.CacheDir, "lyrics", key+".json")
}

// ReadCache 读缓存。
func (c *Client) ReadCache(key string) (*Doc, error) {
	data, err := os.ReadFile(c.CachePath(key))
	if err != nil {
		return nil, ErrNotFound
	}
	var doc Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, ErrNotFound
	}
	if len(doc.Cues) == 0 {
		return nil, ErrNotFound
	}
	return &doc, nil
}

// WriteCache 原子写缓存。
func (c *Client) WriteCache(key string, doc *Doc) error {
	if key == "" || doc == nil || len(doc.Cues) == 0 {
		return nil
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	dir := filepath.Join(c.CacheDir, "lyrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, key+".json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.CachePath(key))
}

// SaveManual 保存用户手动导入/编辑的歌词（缓存里标记 source=manual）。
func (c *Client) SaveManual(key string, doc *Doc, ext string) error {
	if doc == nil {
		return errors.New("空的歌词")
	}
	doc.Source = "manual"
	return c.WriteCache(key, doc)
}

// scoreMatch 给候选打分：标题 > 艺人 > 专辑 > 时长，带时间轴的再加分。
func scoreMatch(title, artist, album string, duration float64, r Record) float64 {
	s := 0.0
	ts := similarity(title, r.TrackName)
	as := similarity(artist, r.ArtistName)
	bs := similarity(album, r.AlbumName)

	s += ts * 120
	if artist != "" {
		s += as * 60
	}
	if album != "" {
		s += bs * 18
	}
	if duration > 0 && r.Duration > 0 {
		d := duration - r.Duration
		if d < 0 {
			d = -d
		}
		switch {
		case d <= 2:
			s += 16
		case d <= 6:
			s += 8
		case d <= 15:
			s -= 4
		default:
			s -= 30
		}
	}
	if r.HasSynced() {
		s += 8
	}
	// 标题完全不像的直接判死，避免「同名专辑里随便挑一首」
	if ts < 0.35 {
		s -= 60
	}
	return s
}

// similarity 返回 0..1 的相似度。中文按「字二元组」算 Dice 系数，
// 英文按词重合，能同时兼容「晴天」vs「晴天 (Live)」和 "Shape of You" vs "shape of you".
func similarity(a, b string) float64 {
	x := normalizeKey(a)
	y := normalizeKey(b)
	if x == "" || y == "" {
		return 0
	}
	if x == y {
		return 1
	}
	if strings.Contains(x, y) || strings.Contains(y, x) {
		if min(len(x), len(y)) >= 2 {
			return 0.72
		}
	}
	gramsA := grams(x)
	gramsB := grams(y)
	if len(gramsA) == 0 || len(gramsB) == 0 {
		return 0
	}
	inter := 0
	for k, v := range gramsA {
		if w, ok := gramsB[k]; ok {
			if v < w {
				inter += v
			} else {
				inter += w
			}
		}
	}
	total := 0
	for _, v := range gramsA {
		total += v
	}
	for _, v := range gramsB {
		total += v
	}
	return 2 * float64(inter) / float64(total)
}

func grams(s string) map[string]int {
	out := map[string]int{}
	rs := []rune(s)
	for i := 0; i+1 < len(rs); i++ {
		out[string(rs[i:i+2])]++
	}
	if len(rs) == 1 {
		out[string(rs)] = 1
	}
	return out
}

func normalizeKey(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r = r - 0xFF01 + 0x21
		case r == 0x3000:
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			(r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF) ||
			(r >= 0xAC00 && r <= 0xD7AF) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func debugLog(format string, args ...any) {
	if os.Getenv("MPLAY_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[mplay] "+format+"\n", args...)
	}
}

// 避免 utf8 导入被优化掉：中文标题长度判断用得上
var _ = utf8.RuneCountInString
