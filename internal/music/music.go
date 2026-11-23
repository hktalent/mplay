// Package music 是在线搜歌的适配器层。
//
// 只接「可以合法公开播放」的源：Apple iTunes 的 30 秒试听、Internet Archive
// 的公有领域/CC 音频、Jamendo 的 CC 音乐。另留一个通用 JSON 音源插件接口，
// 让使用者把自己的接口（自建曲库、私有服务）接进来——代码本身不内置任何
// 第三方平台的音频抓取地址。
package music

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hktalent/mplay/internal/config"
)

// Result 是一条在线搜索结果。
type Result struct {
	Key        string  `json:"key"`
	Source     string  `json:"source"`
	SourceName string  `json:"sourceName"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album,omitempty"`
	Duration   float64 `json:"duration"`
	Year       string  `json:"year,omitempty"`
	Cover      string  `json:"cover,omitempty"`
	Stream     string  `json:"stream,omitempty"`
	Page       string  `json:"page,omitempty"`
	Preview    bool    `json:"preview"`
	Format     string  `json:"format,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// Searcher 负责并发调用各音源并合并结果。
type Searcher struct {
	cfg  *config.Config
	http *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at      time.Time
	results []Result
	errors  []string
}

// New 创建搜索器。
func New(cfg *config.Config) *Searcher {
	return &Searcher{
		cfg: cfg,
		http: &http.Client{
			Timeout: 9 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          32,
				MaxIdleConnsPerHost:   8,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   8 * time.Second,
				ResponseHeaderTimeout: 9 * time.Second,
			},
		},
		cache: map[string]cacheEntry{},
	}
}

// Search 聚合搜索。某个音源失败不会影响其它音源，错误会一并返回给前端展示。
func (s *Searcher) Search(ctx context.Context, q string, limit int) ([]Result, []string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, []string{"搜索词为空"}
	}
	if limit <= 0 {
		limit = 24
	}
	if limit > 60 {
		limit = 60
	}
	key := fmt.Sprintf("%s|%d", strings.ToLower(q), limit)
	s.mu.Lock()
	if c, ok := s.cache[key]; ok && time.Since(c.at) < 5*time.Minute {
		s.mu.Unlock()
		return c.results, c.errors
	}
	s.mu.Unlock()

	type part struct {
		res    []Result
		err    string
		source string
	}
	var parts []part
	var mu sync.Mutex
	var wg sync.WaitGroup

	add := func(name string, fn func(context.Context) ([]Result, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			res, err := fn(c)
			mu.Lock()
			p := part{res: res, source: name}
			if err != nil {
				p.err = fmt.Sprintf("%s: %v", name, err)
			}
			parts = append(parts, p)
			mu.Unlock()
		}()
	}

	if s.cfg.EnableITunes {
		add("Apple 试听", func(c context.Context) ([]Result, error) { return s.searchITunes(c, q, limit) })
	}
	if s.cfg.EnableArchive {
		add("Internet Archive", func(c context.Context) ([]Result, error) { return s.searchArchive(c, q, limit) })
	}
	if s.cfg.JamendoClientID != "" {
		add("Jamendo", func(c context.Context) ([]Result, error) { return s.searchJamendo(c, q, limit) })
	}
	for _, src := range s.cfg.Sources {
		sc := src
		add(sc.Name, func(c context.Context) ([]Result, error) { return s.searchCustom(c, sc, q, limit) })
	}
	wg.Wait()

	var results []Result
	var errs []string
	for _, p := range parts {
		results = append(results, p.res...)
		if p.err != "" {
			errs = append(errs, p.err)
		}
	}
	results = dedupe(results)
	sort.SliceStable(results, func(i, j int) bool {
		// 完整可播放（非试听）优先，其次按来源顺序保持稳定
		return results[i].Preview == false && results[j].Preview == true
	})
	if len(results) > limit {
		results = results[:limit]
	}
	s.mu.Lock()
	s.cache[key] = cacheEntry{at: time.Now(), results: results, errors: errs}
	s.mu.Unlock()
	return results, errs
}

func dedupe(in []Result) []Result {
	seen := map[string]bool{}
	out := make([]Result, 0, len(in))
	for _, r := range in {
		k := strings.ToLower(strings.TrimSpace(r.Title)) + "|" + strings.ToLower(strings.TrimSpace(r.Artist))
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

func (s *Searcher) getJSON(ctx context.Context, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mplay/1.0; +https://m.51pwn.com)")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

// ---------- Apple iTunes Search：30 秒合法试听 ----------

type itunesResp struct {
	Results []struct {
		TrackID       int64  `json:"trackId"`
		TrackName     string `json:"trackName"`
		ArtistName    string `json:"artistName"`
		Collection    string `json:"collectionName"`
		TrackTimeMsec int64  `json:"trackTimeMillis"`
		Artwork       string `json:"artworkUrl100"`
		Preview       string `json:"previewUrl"`
		ViewURL       string `json:"trackViewUrl"`
		ReleaseDate   string `json:"releaseDate"`
	} `json:"results"`
}

func (s *Searcher) searchITunes(ctx context.Context, q string, limit int) ([]Result, error) {
	// 同时查美区与中区：很多华语歌只在其中一个区有试听地址。
	countries := []string{"us", "cn"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []Result
	var firstErr error
	for _, country := range countries {
		country := country
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := url.Values{}
			v.Set("term", q)
			v.Set("media", "music")
			v.Set("entity", "song")
			v.Set("limit", strconv.Itoa(limit))
			v.Set("country", country)
			var r itunesResp
			if err := s.getJSON(ctx, "https://itunes.apple.com/search?"+v.Encode(), nil, &r); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, it := range r.Results {
				if it.Preview == "" {
					continue
				}
				cover := strings.Replace(it.Artwork, "100x100bb", "300x300bb", 1)
				year := ""
				if len(it.ReleaseDate) >= 4 {
					year = it.ReleaseDate[:4]
				}
				out = append(out, Result{
					Key:        fmt.Sprintf("itunes:%d", it.TrackID),
					Source:     "itunes",
					SourceName: "Apple 试听",
					Title:      it.TrackName,
					Artist:     it.ArtistName,
					Album:      it.Collection,
					Duration:   float64(it.TrackTimeMsec) / 1000,
					Year:       year,
					Cover:      cover,
					Stream:     it.Preview,
					Page:       it.ViewURL,
					Preview:    true,
					Format:     "m4a",
					Note:       "30 秒试听（Apple 官方预览）",
				})
			}
		}()
	}
	wg.Wait()
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// ---------- Internet Archive：公有领域 / CC 音频，可完整播放 ----------

func (s *Searcher) searchArchive(ctx context.Context, q string, limit int) ([]Result, error) {
	v := url.Values{}
	v.Set("q", fmt.Sprintf("(%s) AND mediatype:audio", q))
	v.Set("fl[]", "identifier")
	v.Add("fl[]", "title")
	v.Add("fl[]", "creator")
	v.Add("fl[]", "year")
	v.Set("rows", strconv.Itoa(minInt(limit, 12)))
	v.Set("page", "1")
	v.Set("output", "json")
	var resp struct {
		Response struct {
			Docs []struct {
				Identifier any `json:"identifier"`
				Title      any `json:"title"`
				Creator    any `json:"creator"`
				Year       any `json:"year"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := s.getJSON(ctx, "https://archive.org/advancedsearch.php?"+v.Encode(), nil, &resp); err != nil {
		return nil, err
	}

	docs := resp.Response.Docs
	if len(docs) > 6 {
		docs = docs[:6] // 每个条目还要查一次元数据，限制并发量
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []Result
	for _, d := range docs {
		id := asString(d.Identifier)
		if id == "" {
			continue
		}
		meta := map[string]string{
			"title":  asString(d.Title),
			"artist": asString(d.Creator),
			"year":   asString(d.Year),
		}
		wg.Add(1)
		go func(id string, meta map[string]string) {
			defer wg.Done()
			track, err := s.archiveTrack(ctx, id, meta)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, track)
			mu.Unlock()
		}(id, meta)
	}
	wg.Wait()
	if len(out) == 0 {
		return nil, errors.New("没有找到可播放的音频条目")
	}
	return out, nil
}

func (s *Searcher) archiveTrack(ctx context.Context, identifier string, meta map[string]string) (Result, error) {
	var md struct {
		Metadata struct {
			Title   any `json:"title"`
			Creator any `json:"creator"`
		} `json:"metadata"`
		Files []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Length string `json:"length"`
			Title  string `json:"title"`
			Artist string `json:"artist"`
		} `json:"files"`
	}
	if err := s.getJSON(ctx, "https://archive.org/metadata/"+url.PathEscape(identifier), nil, &md); err != nil {
		return Result{}, err
	}
	title := meta["title"]
	if t := asString(md.Metadata.Title); t != "" {
		title = t
	}
	artist := meta["artist"]
	if a := asString(md.Metadata.Creator); a != "" {
		artist = a
	}
	var chosen *struct {
		Name   string `json:"name"`
		Format string `json:"format"`
		Length string `json:"length"`
		Title  string `json:"title"`
		Artist string `json:"artist"`
	}
	for i := range md.Files {
		f := &md.Files[i]
		low := strings.ToLower(f.Format)
		if strings.Contains(low, "mp3") || strings.Contains(low, "ogg") || strings.Contains(low, "mpeg") {
			if chosen == nil || strings.Contains(f.Name, "VBR") {
				chosen = f
			}
			if strings.Contains(low, "vbr mp3") {
				break
			}
		}
	}
	if chosen == nil {
		return Result{}, errors.New("条目里没有音频文件")
	}
	if chosen.Title != "" {
		title = chosen.Title
	}
	if chosen.Artist != "" {
		artist = chosen.Artist
	}
	stream := "https://archive.org/download/" + url.PathEscape(identifier) + "/" + escapePath(chosen.Name)
	return Result{
		Key:        "archive:" + identifier + "/" + chosen.Name,
		Source:     "archive",
		SourceName: "Internet Archive",
		Title:      title,
		Artist:     artist,
		Year:       meta["year"],
		Duration:   parseArchiveLength(chosen.Length),
		Cover:      "https://archive.org/services/img/" + url.PathEscape(identifier),
		Stream:     stream,
		Page:       "https://archive.org/details/" + url.PathEscape(identifier),
		Preview:    false,
		Format:     strings.TrimPrefix(strings.ToLower(fileExt(chosen.Name)), "."),
		Note:       "Internet Archive · 可完整播放",
	}, nil
}

var lengthRe = regexp.MustCompile(`^(\d+):(\d{2})(?::(\d{2}))?`)

func parseArchiveLength(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	m := lengthRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	if m[3] != "" {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		se, _ := strconv.Atoi(m[3])
		return float64(h*3600 + mi*60 + se)
	}
	mi, _ := strconv.Atoi(m[1])
	se, _ := strconv.Atoi(m[2])
	return float64(mi*60 + se)
}

// ---------- Jamendo：CC 音乐，可完整播放 ----------

func (s *Searcher) searchJamendo(ctx context.Context, q string, limit int) ([]Result, error) {
	v := url.Values{}
	v.Set("client_id", s.cfg.JamendoClientID)
	v.Set("format", "json")
	v.Set("limit", strconv.Itoa(minInt(limit, 20)))
	v.Set("search", q)
	v.Set("include", "musicinfo")
	v.Set("audioformat", "mp32")
	var resp struct {
		Results []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Artist    string `json:"artist_name"`
			Album     string `json:"album_name"`
			Duration  int    `json:"duration"`
			Image     string `json:"image"`
			Audio     string `json:"audio"`
			ShareURL  string `json:"shareurl"`
			LicenseCC string `json:"license_ccurl"`
			Released  string `json:"releasedate"`
		} `json:"results"`
	}
	if err := s.getJSON(ctx, "https://api.jamendo.com/v3.0/tracks/?"+v.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Results))
	for _, r := range resp.Results {
		year := ""
		if len(r.Released) >= 4 {
			year = r.Released[:4]
		}
		out = append(out, Result{
			Key:        "jamendo:" + r.ID,
			Source:     "jamendo",
			SourceName: "Jamendo",
			Title:      r.Name,
			Artist:     r.Artist,
			Album:      r.Album,
			Duration:   float64(r.Duration),
			Year:       year,
			Cover:      r.Image,
			Stream:     r.Audio,
			Page:       r.ShareURL,
			Preview:    false,
			Format:     "mp3",
			Note:       "Jamendo · CC 授权，可完整播放",
		})
	}
	return out, nil
}

// ---------- 通用 JSON 音源插件 ----------

func (s *Searcher) searchCustom(ctx context.Context, src config.Source, q string, limit int) ([]Result, error) {
	if strings.TrimSpace(src.Search) == "" {
		return nil, errors.New("音源缺少 searchUrl")
	}
	raw := strings.ReplaceAll(src.Search, "{q}", url.QueryEscape(q))
	raw = strings.ReplaceAll(raw, "{limit}", strconv.Itoa(limit))
	var payload any
	if err := s.getJSON(ctx, raw, src.Headers, &payload); err != nil {
		return nil, err
	}
	items := itemsAt(payload, src.Items)
	if items == nil {
		return nil, fmt.Errorf("itemsPath=%q 没有取到数组", src.Items)
	}
	fields := func(key string) string {
		if src.Fields == nil {
			return ""
		}
		return src.Fields[key]
	}
	out := make([]Result, 0, len(items))
	for i, item := range items {
		title := valueAt(item, fields("title"))
		stream := valueAt(item, fields("stream"))
		if title == "" || stream == "" {
			continue
		}
		if strings.HasPrefix(stream, "/") {
			if u, err := url.Parse(raw); err == nil {
				stream = u.Scheme + "://" + u.Host + stream
			}
		}
		dur := toFloat(valueAt(item, fields("duration")))
		if dur > 10000 { // 毫秒
			dur /= 1000
		}
		out = append(out, Result{
			Key:        fmt.Sprintf("%s:%s", src.ID, url.QueryEscape(title+"|"+valueAt(item, fields("artist"))+fmt.Sprint(i))),
			Source:     src.ID,
			SourceName: src.Name,
			Title:      title,
			Artist:     valueAt(item, fields("artist")),
			Album:      valueAt(item, fields("album")),
			Duration:   dur,
			Cover:      valueAt(item, fields("cover")),
			Stream:     stream,
			Page:       valueAt(item, fields("page")),
			Preview:    false,
			Note:       src.Note,
		})
	}
	return out, nil
}

// itemsAt 取点号路径（"data.list"）上的数组；留空表示根。
func itemsAt(payload any, path string) []any {
	v := walk(payload, path)
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		// 常见包装：{items:[...]} 之类，取第一个数组值
		for _, val := range t {
			if arr, ok := val.([]any); ok {
				return arr
			}
		}
	}
	return nil
}

func valueAt(item any, path string) string {
	if path == "" {
		return ""
	}
	return asString(walk(item, path))
}

func walk(v any, path string) any {
	if path == "" {
		return v
	}
	for _, seg := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[seg]
	}
	return v
}

func toFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// 有些站点把 identifier/title 写成数组，这里统一成字符串。
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		if len(t) > 0 {
			return asString(t[0])
		}
		return ""
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func fileExt(p string) string {
	i := strings.LastIndex(p, ".")
	if i < 0 {
		return ""
	}
	return p[i:]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Sources 返回各音源的启用状态，前端「设置」页展示用。
func (s *Searcher) Sources() []map[string]any {
	out := []map[string]any{
		{"id": "itunes", "name": "Apple 试听", "enabled": s.cfg.EnableITunes, "kind": "builtin", "note": "30 秒试听，合法公开"},
		{"id": "archive", "name": "Internet Archive", "enabled": s.cfg.EnableArchive, "kind": "builtin", "note": "公有领域 / CC 音频"},
		{"id": "jamendo", "name": "Jamendo", "enabled": s.cfg.JamendoClientID != "", "kind": "builtin", "note": "需要配置 client_id"},
	}
	for _, src := range s.cfg.Sources {
		out = append(out, map[string]any{"id": src.ID, "name": src.Name, "enabled": true, "kind": "custom", "note": src.Note})
	}
	return out
}
