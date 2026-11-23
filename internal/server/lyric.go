package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
)

// handleLyric 是「给这首歌配一份歌词」的主入口，按优先级逐个尝试：
// 同名歌词/字幕 → 文件内嵌歌词 → 本地缓存 → LRCLIB →（视频）字幕库。
func (s *Server) handleLyric(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	force := r.URL.Query().Get("force") == "1"
	pick, _ := strconv.ParseInt(r.URL.Query().Get("pick"), 10, 64)

	doc, err := s.resolveLyric(r.Context(), e, force, pick)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": err.Error(),
			"hint":  "可以手动导入 .lrc/.srt/.vtt/.ass，或在设置里打开 LRCLIB",
			"doc":   nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "doc": doc, "source": doc.Source})
}

func (s *Server) resolveLyric(ctx context.Context, e media.Entry, force bool, pick int64) (*lyrics.Doc, error) {
	if pick == 0 {
		if doc, err := s.sidecarDoc(e); err == nil && (doc.Synced || !force) {
			return doc, nil
		}
		if !force {
			if doc, err := s.tagDoc(e); err == nil {
				return doc, nil
			}
			if doc, err := s.o.Lyr.ReadCache(e.ID); err == nil {
				doc.Source = "cache"
				return doc, nil
			}
			if doc, err := s.subsCacheDoc(e); err == nil {
				return doc, nil
			}
		}
	}

	doc, matched, remoteErr := s.o.Lyr.ResolveBest(ctx, e.ID, variantsFor(e), e.Album, e.Duration, force, pick)
	if remoteErr == nil {
		if matched != nil {
			s.applyMatch(e, matched)
		}
		if s.o.Cfg.WriteSidecar && doc.Synced {
			s.writeSidecar(e, doc)
		}
		return doc, nil
	}

	// 视频没有歌词库可用，退到 OpenSubtitles（配置了 key 才生效）
	if e.Kind == "video" {
		if doc, err := s.subsCacheDoc(e); err == nil {
			return doc, nil
		}
		if s.o.Subs.Enabled() {
			if doc, err := s.fetchSubtitle(ctx, e); err == nil {
				return doc, nil
			}
		}
	}

	// 远端失败时，本地已有的（哪怕是纯文本）也好过没有
	if doc, err := s.sidecarDoc(e); err == nil {
		return doc, nil
	}
	if doc, err := s.tagDoc(e); err == nil {
		return doc, nil
	}
	return nil, remoteErr
}

// variantsFor 把「标签 + 文件名的各种拆法」整理成歌词库可校验的候选写法。
// 有内嵌标签时标签排第一（最可信）；只有文件名时按 GuessNames 给的顺序逐个试。
func variantsFor(e media.Entry) []lyrics.Variant {
	var out []lyrics.Variant
	hasRealTags := e.MetaSource != "" && !strings.Contains(e.MetaSource, "filename") && e.MetaSource != "ai"
	if hasRealTags && (e.Title != "" || e.Artist != "") {
		out = append(out, lyrics.Variant{Title: e.Title, Artist: e.Artist, Source: "tag", Library: true})
	}
	for _, g := range media.GuessNames(e.Name) {
		out = append(out, lyrics.Variant{Title: g.Title, Artist: g.Artist, Source: g.Source})
	}
	// 曲库里可能已经被人工/AI 修正过（MetaSource=ai），那条也要保留
	if e.MetaSource == "ai" && e.Title != "" {
		out = append([]lyrics.Variant{{Title: e.Title, Artist: e.Artist, Source: "library", Library: true}}, out...)
	}
	// 去重，最多 6 条（BestMatch 内部也只用到 6 条）
	seen := map[string]bool{}
	var uniq []lyrics.Variant
	for _, v := range out {
		k := media.NormalizeKey(v.Title) + "\x00" + media.NormalizeKey(v.Artist)
		if v.Title == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, v)
	}
	if len(uniq) > 6 {
		uniq = uniq[:6]
	}
	return uniq
}

// applyMatch 把歌词库确认过的歌名/艺人回填到曲库（只改索引，不动用户文件）。
// 只在原来信息是从文件名猜出来的时候回填——真实标签是用户的数据，不覆盖。
func (s *Server) applyMatch(e media.Entry, m *lyrics.Match) {
	if m == nil || strings.TrimSpace(m.Record.TrackName) == "" {
		return
	}
	fromFilename := strings.Contains(e.MetaSource, "filename") || e.MetaSource == "ai" || e.MetaSource == ""
	if !fromFilename {
		return
	}
	title := strings.TrimSpace(m.Record.TrackName)
	artist := strings.TrimSpace(m.Record.ArtistName)
	album := strings.TrimSpace(m.Record.AlbumName)
	if title == "" || (e.Title == title && e.Artist == artist) {
		return
	}
	if s.o.Lib.UpdateMetaFrom(e.ID, title, artist, album, "lrclib") {
		debugf("歌词库回填: %s -> %s - %s", e.Name, title, artist)
	}
}

// sidecarDoc 选一份最合适的同名歌词/字幕，并把另一语言版本合并成译文。
func (s *Server) sidecarDoc(e media.Entry) (*lyrics.Doc, error) {
	if len(e.Sidecars) == 0 {
		return nil, lyrics.ErrNotFound
	}
	type parsed struct {
		sc  media.Sidecar
		doc *lyrics.Doc
	}
	var all []parsed
	for _, sc := range e.Sidecars {
		data, err := os.ReadFile(sc.Path)
		if err != nil {
			continue
		}
		doc, err := lyrics.Parse(data, sc.Ext)
		if err != nil || len(doc.Cues) == 0 {
			continue
		}
		doc.Source = "sidecar"
		all = append(all, parsed{sc: sc, doc: doc})
	}
	if len(all) == 0 {
		return nil, lyrics.ErrNotFound
	}

	score := func(p parsed) int {
		v := 0
		if p.sc.Role == "lyric" {
			v += 4
		}
		if p.doc.Synced {
			v += 3
		}
		if isPreferredLang(p.sc.Lang) {
			v += 2
		}
		return v
	}
	best := 0
	for i := 1; i < len(all); i++ {
		if score(all[i]) > score(all[best]) {
			best = i
		}
	}
	primary := all[best].doc

	// 双语：同曲另一个语言的 lrc 合并成译文
	for i, p := range all {
		if i == best || p.sc.Lang == all[best].sc.Lang {
			continue
		}
		lyrics.MergeTrans(primary, p.doc)
	}
	return primary, nil
}

func isPreferredLang(lang string) bool {
	l := strings.ToLower(lang)
	if l == "" {
		return false
	}
	for _, want := range []string{"zh", "chs", "cht", "cn", "chi", "cmn", "Hans", "hans"} {
		if strings.HasPrefix(l, strings.ToLower(want)) {
			return true
		}
	}
	return false
}

func (s *Server) tagDoc(e media.Entry) (*lyrics.Doc, error) {
	tags := media.ReadTags(e.Path, e.Ext)
	if tags.SyncedLyrics != "" {
		doc, err := lyrics.Parse([]byte(tags.SyncedLyrics), "lrc")
		if err == nil {
			doc.Source = "tag"
			return doc, nil
		}
	}
	if tags.Lyrics != "" {
		doc, err := lyrics.Parse([]byte(tags.Lyrics), "txt")
		if err == nil {
			doc.Source = "tag"
			return doc, nil
		}
	}
	return nil, lyrics.ErrNotFound
}

func (s *Server) subsCacheDoc(e media.Entry) (*lyrics.Doc, error) {
	data, err := s.o.Subs.ReadCache(e.ID)
	if err != nil {
		return nil, lyrics.ErrNotFound
	}
	doc, err := lyrics.Parse(data, ".srt")
	if err != nil {
		return nil, err
	}
	doc.Source = "subtitles"
	return doc, nil
}

func (s *Server) fetchSubtitle(ctx context.Context, e media.Entry) (*lyrics.Doc, error) {
	query := subtitleQuery(e.Name)
	langs := []string{"zh-cn", "zh-tw", "en"}
	if strings.HasPrefix(strings.ToLower(s.o.Cfg.Language), "en") {
		langs = []string{"en", "zh-cn"}
	}
	data, fname, err := s.o.Subs.Fetch(ctx, query, langs)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(fname))
	if ext == "" {
		ext = ".srt"
	}
	doc, perr := lyrics.Parse(data, ext)
	if perr != nil {
		return nil, perr
	}
	doc.Source = "subtitles"
	_ = s.o.Subs.WriteCache(e.ID, data)
	return doc, nil
}

func subtitleQuery(name string) string {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	repl := strings.NewReplacer(".", " ", "_", " ", "【", " ", "】", " ", "[", " ", "]", " ", "(", " ", ")", " ")
	base = repl.Replace(base)
	return strings.Join(strings.Fields(base), " ")
}

// writeSidecar 把远端歌词写成同名 .lrc。
// 只在文件不存在且目录可写时执行，绝不覆盖用户自己的歌词文件。
func (s *Server) writeSidecar(e media.Entry, doc *lyrics.Doc) {
	base := strings.TrimSuffix(e.Path, filepath.Ext(e.Path))
	target := base + ".lrc"
	if _, err := os.Stat(target); err == nil {
		return
	}
	data := []byte(lyrics.ToLRC(doc))
	if err := os.WriteFile(target, data, 0o644); err != nil {
		debugf("写歌词文件失败 %s: %v", target, err)
		return
	}
	debugf("已把远端歌词写入 %s", target)
}

// handleLyricCandidates 返回 LRCLIB 的候选歌词，让用户手动挑一份更准的。
func (s *Server) handleLyricCandidates(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	cands, err := s.o.Lyr.Candidates(r.Context(), e.Title, e.Artist, e.Album, e.Duration)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "candidates": []any{}})
		return
	}
	type cand struct {
		ID        int64   `json:"id"`
		Track     string  `json:"trackName"`
		Artist    string  `json:"artistName"`
		Album     string  `json:"albumName"`
		Duration  float64 `json:"duration"`
		HasSynced bool    `json:"hasSynced"`
		Score     float64 `json:"score"`
	}
	out := make([]cand, 0, len(cands))
	for _, c := range cands {
		out = append(out, cand{c.ID, c.TrackName, c.ArtistName, c.AlbumName, c.Duration, c.HasSynced(), c.Score})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidates": out})
}

// handleLyricUpload 接收用户手动导入的歌词/字幕（原始文本，扩展名走 query）。
func (s *Server) handleLyricUpload(w http.ResponseWriter, r *http.Request) {
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "曲目不存在"})
		return
	}
	ext := r.URL.Query().Get("ext")
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil || len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "空文件或读取失败"})
		return
	}
	if ext == "" {
		ext = lyrics.Detect(lyrics.DecodeText(body))
	}
	doc, perr := lyrics.Parse(body, ext)
	if perr != nil {
		errJSON(w, http.StatusBadRequest, perr, "支持 .lrc / .srt / .vtt / .ass / .txt")
		return
	}
	doc.Source = "manual"
	if err := s.o.Lyr.SaveManual(e.ID, doc, ext); err != nil {
		errJSON(w, http.StatusInternalServerError, err, "写缓存失败")
		return
	}
	if strings.HasPrefix(strings.ToLower(ext), "srt") {
		_ = s.o.Subs.WriteCache(e.ID, body)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "doc": doc})
}

func debugf(format string, args ...any) {
	if os.Getenv("MPLAY_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[mplay] "+format+"\n", args...)
	}
}
