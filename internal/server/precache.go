package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/hktalent/mplay/internal/media"
)

// PrecacheStats 是后台「自动缓存歌词」的进度快照。
// 曲库列表里的歌会按顺序在后台补歌词与元信息，前端只读这个结构画进度。
type PrecacheStats struct {
	Running    bool   `json:"running"`
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	Hit        int    `json:"hit"`
	Miss       int    `json:"miss"`
	Skipped    int    `json:"skipped"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	LastItem   string `json:"lastItem"`
	LastError  string `json:"lastError"`
}

// precacheWorkers 是后台并发度。LRCLIB 有人肉限流（客户端已节流到 250ms/次），
// 2 个协程足够把一整库跑完，又不会把公共接口打挂。
const precacheWorkers = 2

func (s *Server) precacheState() PrecacheStats {
	s.preMu.Lock()
	defer s.preMu.Unlock()
	return s.pre
}

func (s *Server) precacheWorker() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.preStop:
			return
		case <-s.preKick:
		case <-t.C:
		}
		s.maybePrecache()
	}
}

// KickPrecache 请求尽快跑一轮（扫描完成后、或用户点「立即缓存」时调用）。
func (s *Server) KickPrecache() {
	select {
	case s.preKick <- struct{}{}:
	default:
	}
}

// maybePrecache 判断现在该不该跑一轮后台缓存。
func (s *Server) maybePrecache() {
	if s == nil || s.o.Lib == nil || s.o.Lyr == nil {
		return
	}
	if !s.o.Cfg.AutoCacheLyrics || !s.o.Lyr.Enabled {
		return
	}
	if s.o.Lib.Scanning() {
		return // 扫描中先等一轮，别和扫描抢磁盘
	}
	if s.precacheState().Running {
		return
	}
	pending := s.pendingLyricEntries()
	if len(pending) == 0 {
		return
	}
	s.runPrecache(pending)
}

// pendingLyricEntries 找出「还没歌词、值得去歌词库问一遍」的音频条目。
func (s *Server) pendingLyricEntries() []media.Entry {
	items := s.o.Lib.List()
	out := make([]media.Entry, 0, len(items))
	for _, e := range items {
		if e.Kind != "audio" {
			continue
		}
		if e.LyricSource != "" || e.CueCount > 0 {
			continue // 同名歌词/内嵌歌词已经有了
		}
		if doc, err := s.o.Lyr.ReadCache(e.ID); err == nil && doc != nil && len(doc.Cues) > 0 {
			continue // 远端缓存已命中
		}
		out = append(out, e)
	}
	return out
}

func (s *Server) runPrecache(items []media.Entry) {
	s.preMu.Lock()
	s.pre = PrecacheStats{Running: true, Total: len(items), StartedAt: time.Now().Unix()}
	s.preMu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, precacheWorkers)
	for _, e := range items {
		if !s.precacheAllowed() {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e media.Entry) {
			defer wg.Done()
			defer func() { <-sem }()
			if !s.precacheAllowed() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			s.cacheOne(ctx, e)
		}(e)
	}
	wg.Wait()

	s.preMu.Lock()
	s.pre.Running = false
	s.pre.FinishedAt = time.Now().Unix()
	s.pre.LastItem = ""
	s.preMu.Unlock()
}

// cacheOne 给单曲补歌词；成功时顺便把歌词库确认过的歌名/艺人回填进曲库。
func (s *Server) cacheOne(ctx context.Context, e media.Entry) {
	s.preMu.Lock()
	s.pre.LastItem = e.Name
	s.preMu.Unlock()

	done := func(hit bool) {
		s.preMu.Lock()
		if hit {
			s.pre.Hit++
		} else {
			s.pre.Miss++
		}
		s.pre.Done++
		s.preMu.Unlock()
	}

	doc, err := s.resolveLyric(ctx, e, false, 0)
	if err != nil || doc == nil || len(doc.Cues) == 0 {
		if err != nil && ctx.Err() == nil {
			s.preMu.Lock()
			s.pre.LastError = err.Error()
			s.preMu.Unlock()
		}
		done(false)
		return
	}
	done(true)
	// 让前端列表能立刻看到「有歌词」的角标
	if item, ok := s.o.Lib.Get(e.ID); ok && item.CueCount == 0 {
		s.o.Lib.UpdateLyricInfo(e.ID, doc.Source, doc.Synced, len(doc.Cues))
	}
}

func (s *Server) precacheAllowed() bool {
	if !s.precacheState().Running {
		return false
	}
	if !s.o.Cfg.AutoCacheLyrics || !s.o.Lyr.Enabled {
		return false
	}
	return !s.o.Lib.Scanning()
}

func (s *Server) handleCacheStatus(w http.ResponseWriter, r *http.Request) {
	st := s.precacheState()
	stats := s.o.Lib.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"enabled":   s.o.Cfg.AutoCacheLyrics,
		"lrclib":    s.o.Lyr.Enabled,
		"pending":   len(s.pendingLyricEntries()),
		"progress":  st,
		"audio":     stats["audio"],
		"withLyric": stats["withLyric"],
	})
}

func (s *Server) handleCacheRun(w http.ResponseWriter, r *http.Request) {
	if !s.o.Cfg.AutoCacheLyrics {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "自动缓存歌词已关闭，先在设置里打开"})
		return
	}
	if !s.o.Lyr.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "歌词库（LRCLIB）未启用"})
		return
	}
	if s.precacheState().Running {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "already": true, "pending": len(s.pendingLyricEntries()), "progress": s.precacheState()})
		return
	}
	items := s.pendingLyricEntries()
	if len(items) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": 0, "hint": "所有曲目都已经有歌词了"})
		return
	}
	s.KickPrecache()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "pending": len(items)})
}
