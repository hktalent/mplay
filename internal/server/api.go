package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"mime"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hktalent/mplay/internal/config"
	"github.com/hktalent/mplay/internal/desktop"
	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
	"github.com/hktalent/mplay/internal/music"
)

// ---------- 健康检查 / 配置 ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"version":  s.o.Version,
		"desktop":  s.o.Desktop,
		"platform": config.Platform(),
		"uptime":   time.Since(s.start).Round(time.Second).String(),
		"stats":    s.o.Lib.Stats(),
		"roots":    s.o.Cfg.MediaDirs,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": s.o.Version, "api": "mplay/v1"})
}

func (s *Server) publicConfig() map[string]any {
	cfg := s.o.Cfg
	return map[string]any{
		"addr":                cfg.Addr,
		"mediaDirs":           cfg.MediaDirs,
		"writeSidecar":        cfg.WriteSidecar,
		"enableITunes":        cfg.EnableITunes,
		"enableArchive":       cfg.EnableArchive,
		"enableLRCLIB":        cfg.EnableLRCLIB,
		"enableKugou":         cfg.EnableKugou,
		"enableNeteaseSource": cfg.EnableNeteaseSource,
		"enableNeteaseLyric":  cfg.EnableNeteaseLyric,
		"allowLan":            cfg.AllowLAN,
		"lrclibBase":          cfg.LRCLIBBase,
		"jamendoConfigured":   cfg.JamendoClientID != "",
		"subtitlesConfigured": cfg.OSAPIKey != "",
		"language":            cfg.Language,
		"autoCacheLyrics":     cfg.AutoCacheLyrics,
		"aiIdentify":          cfg.AIIdentify,
		"aiBaseUrl":           cfg.AIBaseURL,
		"aiModel":             cfg.AIModel,
		"aiUploadAudio":       cfg.AIUploadAudio,
		"aiConfigured":        strings.TrimSpace(cfg.AIAPIKey) != "",
		"sources":             cfg.Sources,
		"authRequired":        cfg.AuthToken != "",
		"desktop":             s.o.Desktop,
		"platform":            config.Platform(),
		"configPath":          cfg.Path(),
		"version":             s.o.Version,
	}
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.publicConfig())
}

type configPatch struct {
	MediaDirs           *[]string        `json:"mediaDirs"`
	WriteSidecar        *bool            `json:"writeSidecar"`
	EnableITunes        *bool            `json:"enableITunes"`
	EnableArchive       *bool            `json:"enableArchive"`
	EnableLRCLIB        *bool            `json:"enableLRCLIB"`
	EnableKugou         *bool            `json:"enableKugou"`
	EnableNeteaseSource *bool            `json:"enableNeteaseSource"`
	EnableNeteaseLyric  *bool            `json:"enableNeteaseLyricSearch"`
	JamendoClientID     *string          `json:"jamendoClientId"`
	OpenSubtitlesAPIKey *string          `json:"opensubtitlesApiKey"`
	OpenSubtitlesUser   *string          `json:"opensubtitlesUsername"`
	OpenSubtitlesPass   *string          `json:"opensubtitlesPassword"`
	Language            *string          `json:"language"`
	AutoCacheLyrics     *bool            `json:"autoCacheLyrics"`
	AIIdentify          *bool            `json:"aiIdentify"`
	AIBaseURL           *string          `json:"aiBaseUrl"`
	AIAPIKey            *string          `json:"aiApiKey"`
	AIModel             *string          `json:"aiModel"`
	AIUploadAudio       *bool            `json:"aiUploadAudio"`
	Sources             *[]config.Source `json:"sources"`
	AuthToken           *string          `json:"authToken"`
}

func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	// 配置是内存里的共享对象：并发 POST（前端连点两次「移除」）会互相覆盖并
	// 同时写同一个临时文件，导致 os.Rename 失败报「配置文件写入失败」。
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	var p configPatch
	if err := decodeBody(r, &p); err != nil {
		errJSON(w, http.StatusBadRequest, err, "请求体必须是 JSON")
		return
	}
	cfg := s.o.Cfg
	dirsChanged := false
	if p.MediaDirs != nil {
		before := strings.Join(cfg.MediaDirs, "\n")
		cfg.MediaDirs = *p.MediaDirs
		cfg.Normalize()
		dirsChanged = before != strings.Join(cfg.MediaDirs, "\n")
	}
	if p.WriteSidecar != nil {
		cfg.WriteSidecar = *p.WriteSidecar
	}
	if p.EnableITunes != nil {
		cfg.EnableITunes = *p.EnableITunes
	}
	if p.EnableArchive != nil {
		cfg.EnableArchive = *p.EnableArchive
	}
	if p.EnableLRCLIB != nil {
		cfg.EnableLRCLIB = *p.EnableLRCLIB
		s.o.Lyr.Enabled = *p.EnableLRCLIB
	}
	if p.EnableKugou != nil {
		cfg.EnableKugou = *p.EnableKugou
		s.o.Lyr.Kugou = *p.EnableKugou
	}
	if p.EnableNeteaseSource != nil {
		cfg.EnableNeteaseSource = *p.EnableNeteaseSource
		s.o.Lyr.Netease = *p.EnableNeteaseSource
	}
	if p.EnableNeteaseLyric != nil {
		cfg.EnableNeteaseLyric = *p.EnableNeteaseLyric
	}
	if p.JamendoClientID != nil {
		cfg.JamendoClientID = strings.TrimSpace(*p.JamendoClientID)
	}
	if p.OpenSubtitlesAPIKey != nil {
		cfg.OSAPIKey = strings.TrimSpace(*p.OpenSubtitlesAPIKey)
		if s.o.Subs != nil {
			s.o.Subs.APIKey = cfg.OSAPIKey
		}
	}
	if p.OpenSubtitlesUser != nil {
		cfg.OSUsername = strings.TrimSpace(*p.OpenSubtitlesUser)
		if s.o.Subs != nil {
			s.o.Subs.Username = cfg.OSUsername
		}
	}
	if p.OpenSubtitlesPass != nil {
		cfg.OSPassword = strings.TrimSpace(*p.OpenSubtitlesPass)
		if s.o.Subs != nil {
			s.o.Subs.Password = cfg.OSPassword
		}
	}
	if p.Language != nil {
		cfg.Language = *p.Language
	}
	if p.Sources != nil {
		cfg.Sources = *p.Sources
	}
	if p.AutoCacheLyrics != nil {
		cfg.AutoCacheLyrics = *p.AutoCacheLyrics
		if *p.AutoCacheLyrics {
			s.KickPrecache()
		}
	}
	if p.AuthToken != nil {
		cfg.AuthToken = strings.TrimSpace(*p.AuthToken)
	}
	if p.AIIdentify != nil {
		cfg.AIIdentify = *p.AIIdentify
	}
	if p.AIBaseURL != nil {
		cfg.AIBaseURL = strings.TrimSpace(*p.AIBaseURL)
	}
	if p.AIModel != nil {
		cfg.AIModel = strings.TrimSpace(*p.AIModel)
	}
	if p.AIUploadAudio != nil {
		cfg.AIUploadAudio = *p.AIUploadAudio
	}
	if p.AIAPIKey != nil {
		// 前端留空表示「保持原样」，避免每次改开关就把 key 清掉
		if v := strings.TrimSpace(*p.AIAPIKey); v != "" {
			cfg.AIAPIKey = v
		}
	}
	cfg.Normalize()
	if err := cfg.Save(); err != nil {
		errJSON(w, http.StatusInternalServerError, err, "配置文件写入失败")
		return
	}
	if dirsChanged {
		// 必须走 RequestScan：连续移除多个目录时，前一次扫描可能还没结束，
		// 直接 Scan 会返回「扫描正在进行中」并把这次改动丢掉（曲库残留旧条目）。
		s.o.Lib.RequestScan()
	}
	writeJSON(w, http.StatusOK, s.publicConfig())
}

// ---------- 曲库 ----------

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	kind := r.URL.Query().Get("kind")
	filter := r.URL.Query().Get("filter")
	sortBy := r.URL.Query().Get("sort")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}

	items := s.o.Lib.List()
	out := make([]media.Entry, 0, len(items))
	for _, e := range items {
		if kind != "" && kind != "all" && e.Kind != kind {
			continue
		}
		switch filter {
		case "lyric":
			if e.LyricSource == "" {
				continue
			}
		case "nolyric":
			if e.LyricSource != "" {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.Artist+" "+e.Album+" "+e.Name+" "+e.Path), q) {
			continue
		}
		out = append(out, e)
	}

	switch sortBy {
	case "artist":
		sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Artist) < strings.ToLower(out[j].Artist) })
	case "newest":
		sort.SliceStable(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	case "size":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	case "duration":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Duration > out[j].Duration })
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out,
		"total": total,
		"stats": s.o.Lib.Stats(),
		"roots": s.o.Cfg.MediaDirs,
	})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	// 同样用 RequestScan：正在扫描时这次点击会被排队补扫，而不是直接忽略。
	s.o.Lib.RequestScan()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "scanning": s.o.Lib.Scanning()})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.o.Lib.Stats())
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": e})
}

func (s *Server) handleDuration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Duration float64 `json:"duration"`
	}
	if err := decodeBody(r, &body); err != nil {
		errJSON(w, http.StatusBadRequest, err, "")
		return
	}
	s.o.Lib.UpdateDuration(r.PathValue("id"), body.Duration)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 媒体流 ----------

func contentTypes() map[string]string {
	return map[string]string{
		".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".m4b": "audio/mp4",
		".aac": "audio/aac", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
		".wav": "audio/wav", ".ape": "audio/x-ape", ".wma": "audio/x-ms-wma",
		".aiff": "audio/aiff", ".aif": "audio/aiff", ".alac": "audio/mp4",
		".mp4": "video/mp4", ".m4v": "video/mp4", ".mkv": "video/x-matroska",
		".webm": "video/webm", ".mov": "video/quicktime", ".avi": "video/x-msvideo",
		".ts": "video/mp2t", ".flv": "video/x-flv", ".wmv": "video/x-ms-wmv",
	}
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	f, err := os.Open(e.Path)
	if err != nil {
		errJSON(w, http.StatusNotFound, err, "文件已被移动或删除，请重新扫描曲库")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err, "")
		return
	}
	if ct := contentTypes()[e.Ext]; ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// 中文文件名用 RFC 5987 编码，Safari/Chrome 都能正确保存
	disposition := mime.FormatMediaType("inline", map[string]string{"filename": e.Name})
	if disposition == "" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "private, max-age=600")
	// ServeContent 自带 Range / If-Modified-Since 支持，视频拖动进度条依赖这一点
	http.ServeContent(w, r, e.Name, st.ModTime(), f)
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, mimeType, err := s.o.Lib.Cover(e)
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// ---------- 在线搜索 ----------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ctx := r.Context()
	results, errs := s.o.Search.Search(ctx, q, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"results": results,
		"errors":  errs,
		"sources": s.o.Search.Sources(),
	})
}

func (s *Server) handleSearchLyrics(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 q 参数"})
		return
	}
	s.ensureSnippetIndex(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	local := s.snippets.Search(q, 30)
	var remote []music.LyricSnippet
	var library []lyricRecord
	var errs []string

	if s.o.Cfg.EnableNeteaseLyric {
		if res, err := s.o.Search.SearchLyricSnippets(ctx, q, 10); err != nil {
			errs = append(errs, "歌词搜索: "+err.Error())
		} else {
			remote = res
		}
	}

	// LRCLIB 只能按歌名/歌手匹配，作为第三层兜底：用户输入的片段偶尔正好是歌名
	if s.o.Lyr.Enabled {
		if recs, err := s.o.Lyr.SearchQuery(ctx, q); err != nil {
			errs = append(errs, "LRCLIB: "+err.Error())
		} else {
			for _, rec := range recs {
				if len(library) >= 12 {
					break
				}
				matched, snippet := matchLyricLines(rec, q)
				library = append(library, lyricRecord{
					ID: rec.ID, Track: rec.TrackName, Artist: rec.ArtistName, Album: rec.AlbumName,
					Duration: rec.Duration, HasSynced: rec.HasSynced(), Matched: matched, Snippet: snippet,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query":      q,
		"local":      local,
		"remote":     remote,
		"library":    library,
		"errors":     errs,
		"indexLines": s.snippets.Count(),
	})
}

// lyricRecord 是 LRCLIB 名称匹配的结果。
type lyricRecord struct {
	ID        int64    `json:"id"`
	Track     string   `json:"trackName"`
	Artist    string   `json:"artistName"`
	Album     string   `json:"albumName"`
	Duration  float64  `json:"duration"`
	HasSynced bool     `json:"hasSynced"`
	Matched   []string `json:"matched"`
	Snippet   string   `json:"snippet"`
}

func matchLyricLines(rec lyrics.Record, q string) ([]string, string) {
	raw := rec.PlainLyrics
	if raw == "" {
		raw = rec.SyncedLyrics
	}
	nq := normalizeQuery(q)
	var matched []string
	var snippet string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(stripLRCTime(line))
		if line == "" {
			continue
		}
		if nq == "" || strings.Contains(normalizeQuery(line), nq) {
			if len(matched) < 3 {
				matched = append(matched, line)
			}
			if snippet == "" {
				snippet = line
			}
		}
	}
	if snippet == "" && len(raw) > 0 {
		lines := strings.Split(strings.TrimSpace(raw), "\n")
		snippet = strings.TrimSpace(stripLRCTime(lines[0]))
	}
	return matched, snippet
}

func normalizeQuery(s string) string {
	// 只留字母数字与汉字，忽略大小写/标点，避免「，」和「,」的差异
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 0x4E00 && r <= 0x9FFF) ||
			(r >= 0x3040 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7AF) ||
			r >= 0x3400 && r <= 0x4DBF {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func stripLRCTime(line string) string {
	// 去掉行首所有 [mm:ss.xx]
	for {
		if !strings.HasPrefix(line, "[") {
			return line
		}
		if i := strings.Index(line, "]"); i > 0 {
			inside := line[1:i]
			if strings.Contains(inside, ":") {
				line = line[i+1:]
				continue
			}
		}
		return line
	}
}

func (s *Server) handleRemoteLyric(w http.ResponseWriter, r *http.Request) {
	track := strings.TrimSpace(r.URL.Query().Get("track"))
	artist := strings.TrimSpace(r.URL.Query().Get("artist"))
	album := strings.TrimSpace(r.URL.Query().Get("album"))
	duration, _ := strconv.ParseFloat(r.URL.Query().Get("duration"), 64)
	pick, _ := strconv.ParseInt(r.URL.Query().Get("pick"), 10, 64)
	if track == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 track 参数"})
		return
	}
	key := "remote-" + shortHash(strings.ToLower(track+"|"+artist+"|"+strconv.Itoa(int(duration))))
	doc, err := s.o.Lyr.Resolve(r.Context(), key, track, artist, album, duration, false, pick)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "doc": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "doc": doc})
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:10]
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.o.Search.Sources()})
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := decodeBody(r, &body); err != nil {
		errJSON(w, http.StatusBadRequest, err, "")
		return
	}
	if !s.o.Desktop {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "只有桌面版可以调用系统播放器/文件管理器"})
		return
	}
	e, ok := s.o.Lib.Get(body.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	var err error
	switch body.Action {
	case "player":
		err = desktop.OpenWithSystemPlayer(e.Path)
	case "reveal", "":
		err = desktop.Reveal(e.Path)
	default:
		err = errors.New("未知操作: " + body.Action)
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
