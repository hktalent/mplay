package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hktalent/mplay/internal/identify"
	"github.com/hktalent/mplay/internal/media"
)

// 识别链路的统一入口：本地候选 → 歌词库校验 → （可选）AI 兜底 → 再校验。
// 无论走哪条路，只有校验通过（或 AI 明确给出歌名）的答案才会被写进曲库。

type identifyBody struct {
	Apply       *bool `json:"apply"`       // 是否把结果写进曲库（默认 false，前端确认后传 true）
	Audio       *bool `json:"audio"`       // 是否允许这次上传音频片段（还要配置里允许）
	FetchLyrics *bool `json:"fetchLyrics"` // 应用后是否立即按新歌名重拉歌词
	Limit       *int  `json:"limit"`       // 批量识别时的上限
	OnlyMissing *bool `json:"onlyMissing"` // 批量时只处理缺元信息的条目
}

func (s *Server) aiClient() *identify.Client {
	c := s.o.Cfg
	if !c.AIIdentify || strings.TrimSpace(c.AIBaseURL) == "" || strings.TrimSpace(c.AIModel) == "" {
		return nil
	}
	return identify.New(identify.Options{
		Enabled:       true,
		BaseURL:       c.AIBaseURL,
		APIKey:        strings.TrimSpace(c.AIAPIKey),
		Model:         c.AIModel,
		AllowAudio:    c.AIUploadAudio,
		MaxAudioBytes: 6 << 20,
		Timeout:       30 * time.Second,
	}, nil)
}

func hintsFor(e media.Entry) identify.Hints {
	h := identify.Hints{
		Name:         e.Name,
		Path:         e.Path,
		Ext:          e.Ext,
		Duration:     e.Duration,
		LyricSnippet: lyricSnippet(e),
	}
	// 后端在没有内嵌标签时会把文件名猜的结果写进 Title/Artist（MetaSource=filename），
	// 那不是真标签，不能再当"标签线索"喂给识别，否则会把错误的猜测复述一遍。
	if e.MetaSource != "filename" {
		h.Title, h.Artist, h.Album = e.Title, e.Artist, e.Album
	}
	return h
}

// lyricSnippet 取已有歌词/字幕的头几行。中文资源站的文件名经常是乱的，
// 但歌词第一句往往就是歌名，这条线索很值钱。
func lyricSnippet(e media.Entry) string {
	for _, sc := range e.Sidecars {
		if sc.Role != "lyric" {
			continue
		}
		data, err := readFileLimited(sc.Path, 512<<10)
		if err != nil {
			continue
		}
		if t := firstLines(string(data), 6); t != "" {
			return t
		}
	}
	return ""
}

// readFileLimited 读文件前 max 字节，避免把超大字幕整份读进内存。
func readFileLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, n)
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '[' || l[0] == '#' || l[0] == '0' {
			continue
		}
		// 去掉 "[00:12.34]歌词" 这类时间轴前缀
		if strings.HasPrefix(l, "0") && strings.Contains(l, "]") {
			l = strings.TrimSpace(l[strings.Index(l, "]")+1:])
		}
		if l == "" {
			continue
		}
		out = append(out, l)
		if len(out) >= n {
			break
		}
	}
	return strings.Join(out, " / ")
}

// bestGuess 跑完整条链路，返回最好的候选与所有候选（前端可展示选择）。
func (s *Server) bestGuess(ctx context.Context, e media.Entry, wantAudio bool) (identify.Candidate, []identify.Candidate, string) {
	h := hintsFor(e)
	cands := identify.LocalCandidates(h)

	best := identify.Candidate{}
	all := make([]identify.Candidate, 0, len(cands)+1)
	verified := false
	for i, c := range cands {
		if i >= 3 { // 只校验前三条，避免打爆歌词库
			break
		}
		v, ok := identify.Verify(ctx, s.o.Lyr, c, h.Duration)
		v.Source = c.Source
		if ok {
			v.Source = "lrclib"
			v.Note = "歌词库校验通过：" + c.Source
		}
		all = append(all, v)
		if ok && (!verified || v.Confidence > best.Confidence) {
			best, verified = v, true
		}
	}

	aiErr := ""
	// 本地+歌词库已经有高置信度结论时就不用 AI，省调用也省上传。
	needAI := !verified || best.Confidence < 0.8
	if needAI {
		if ai := s.aiClient(); ai != nil {
			c, err := ai.Ask(ctx, h, wantAudio)
			if err != nil {
				aiErr = err.Error()
			} else {
				cand := c
				// AI 的答案也过一遍歌词库，避免编造专辑/错歌手
				v, ok := identify.Verify(ctx, s.o.Lyr, cand, h.Duration)
				v.Source = cand.Source
				if ok {
					v.Note = "AI 答案经歌词库校验"
				} else {
					v.Note = strings.TrimSpace(cand.Note + "（未经歌词库校验）")
					// 未校验的 AI 答案压一压可信度，避免盖掉可信标签
					v.Confidence = minFloat(cand.Confidence, 0.55)
				}
				all = append(all, v)
				// 未校验的 AI 答案也允许顶上来（单曲识别是用户主动点的），
				// 但可信度已被压到 0.55，前端会标注「未经歌词库校验」。
				if ok || v.Title != "" {
					if v.Confidence >= best.Confidence {
						best = v
						verified = ok
					}
				}
			}
		}
	}
	return best, all, aiErr
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// handleIdentifyTrack 处理单曲识别：POST /api/track/{id}/identify
func (s *Server) handleIdentifyTrack(w http.ResponseWriter, r *http.Request) {
	var b identifyBody
	_ = decodeBody(r, &b)
	e, ok := s.o.Lib.Get(r.PathValue("id"))
	if !ok {
		errJSON(w, http.StatusNotFound, nil, "曲目不存在")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	best, all, aiErr := s.bestGuess(ctx, e, b.Audio != nil && *b.Audio)
	// 只有“校验过”或“AI 自己很确信”的结果才准许写回曲库：
	// 没把握的猜测（比如 AI 只给了 0.55 的泛化答案）写进去等于用噪声覆盖原来的文件名线索。
	trusted := best.Verified || best.Confidence >= 0.75
	resp := map[string]any{
		"candidates": all,
		"best":       best,
		"verified":   best.Verified,
		"aiError":    aiErr,
		"apply":      false,
	}
	if b.Apply != nil && *b.Apply && best.Title != "" && !trusted {
		resp["error"] = "结果没把握，未写回：最接近 " + strings.TrimSpace(best.Title+" - "+best.Artist)
	}
	if aiErr != "" && len(all) <= 2 && !best.Verified {
		// 只剩文件名那两条且 AI 也失败：明确告诉前端为什么没结果
		resp["error"] = "没有足够可靠的结果：" + aiErr
	}

	if b.Apply != nil && *b.Apply && best.Title != "" && trusted {
		if s.o.Lib.UpdateMeta(e.ID, best.Title, best.Artist, best.Album) {
			resp["apply"] = true
			if b.FetchLyrics == nil || *b.FetchLyrics {
				if item, ok := s.o.Lib.Get(e.ID); ok {
					if doc, err := s.resolveLyric(ctx, item, true, 0); err == nil && doc != nil {
						resp["lyric"] = map[string]any{
							"source": doc.Source,
							"synced": doc.Synced,
							"lines":  len(doc.Cues),
						}
						resp["item"] = item
					}
				}
			}
			if _, ok := resp["item"]; !ok {
				if item, ok := s.o.Lib.Get(e.ID); ok {
					resp["item"] = item
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAICheck 只验证端点是否可用（设置页「测试连通」）。
func (s *Server) handleAICheck(w http.ResponseWriter, r *http.Request) {
	ai := s.aiClient()
	if ai == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "hint": "先在设置里填接口地址、模型名并开启 AI 识别"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := ai.Ping(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "hint": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": s.o.Cfg.AIModel})
}

// handleIdentifyLibrary 批量识别：POST /api/library/identify
// 默认只处理「缺歌手/标题，或者标题是从文件名猜的」条目，逐条顺序跑，不并发轰炸接口。
func (s *Server) handleIdentifyLibrary(w http.ResponseWriter, r *http.Request) {
	var b identifyBody
	if err := decodeBody(r, &b); err != nil {
		errJSON(w, http.StatusBadRequest, err, "请求体必须是 JSON")
		return
	}
	limit := 20
	if b.Limit != nil && *b.Limit > 0 {
		limit = *b.Limit
	}
	if limit > 100 {
		limit = 100
	}
	onlyMissing := true
	if b.OnlyMissing != nil {
		onlyMissing = *b.OnlyMissing
	}
	apply := true
	if b.Apply != nil {
		apply = *b.Apply
	}

	start := time.Now()
	items := s.o.Lib.List()
	results := make([]map[string]any, 0, limit)
	processed, updated, failed := 0, 0, 0

	for _, e := range items {
		if len(results) >= limit || time.Since(start) > 90*time.Second {
			break
		}
		if e.Kind != "audio" {
			continue
		}
		if onlyMissing && e.Artist != "" && e.Title != "" && e.MetaSource != "filename" {
			continue
		}
		processed++
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		best, _, aiErr := s.bestGuess(ctx, e, false) // 批量里不传音频，避免把整个曲库传上去
		cancel()

		row := map[string]any{
			"id":       e.ID,
			"name":     e.Name,
			"title":    best.Title,
			"artist":   best.Artist,
			"album":    best.Album,
			"source":   best.Source,
			"verified": best.Verified,
			"conf":     best.Confidence,
		}
		switch {
		case best.Verified && apply:
			if s.o.Lib.UpdateMeta(e.ID, best.Title, best.Artist, best.Album) {
				updated++
				row["applied"] = true
			}
		case best.Verified:
			row["applied"] = false
		default:
			failed++
			if aiErr != "" {
				row["error"] = aiErr
			} else {
				row["error"] = "没有可靠结果"
			}
		}
		results = append(results, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"processed": processed,
		"updated":   updated,
		"failed":    failed,
		"aiReady":   s.aiClient() != nil,
		"lrclib":    s.o.Lyr != nil && s.o.Lyr.Enabled,
		"items":     results,
		"ok":        true,
	})
}
