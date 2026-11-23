package server

import (
	"context"
	"runtime"
	"sync"

	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
)

// ensureSnippetIndex 在曲库扫描结果变化后重建歌词全文索引。
// 只读本地已有的歌词（同名文件 / 内嵌 / 缓存），不会为了建索引去联网拉歌词，
// 所以首屏搜索不会被网络拖慢；之后拉到的新歌词会由 AddDoc 增量补进去。
func (s *Server) ensureSnippetIndex(ctx context.Context) {
	scanned, _ := s.o.Lib.Stats()["scannedAt"].(int64)
	s.idxMu.Lock()
	defer s.idxMu.Unlock()
	if s.idxBuiltAt == scanned && s.snippets.Count() > 0 {
		return
	}

	entries := s.o.Lib.List()
	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 6 {
		workers = 6
	}
	jobs := make(chan media.Entry)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var items []lyrics.Snippet

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				doc, ok := s.localLyricDoc(e)
				if !ok || len(doc.Cues) == 0 {
					continue
				}
				rows := make([]lyrics.Snippet, 0, len(doc.Cues))
				for _, c := range doc.Cues {
					rows = append(rows, lyrics.Snippet{
						MediaID: e.ID, Title: e.Title, Artist: e.Artist, Kind: e.Kind,
						Start: c.Start, End: c.End, Text: c.Text, Trans: c.Trans,
					})
				}
				mu.Lock()
				items = append(items, rows...)
				mu.Unlock()
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, e := range entries {
			if e.LyricSource == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case jobs <- e:
			}
		}
	}()
	wg.Wait()

	s.snippets.Replace(items, scanned)
	s.idxBuiltAt = scanned
	debugf("歌词索引已重建：%d 首歌、%d 行", len(entries), len(items))
}

// localLyricDoc 只取本地来源的歌词，用于建索引（不触发远端请求）。
func (s *Server) localLyricDoc(e media.Entry) (*lyrics.Doc, bool) {
	if doc, err := s.sidecarDoc(e); err == nil && len(doc.Cues) > 0 {
		return doc, true
	}
	if doc, err := s.tagDoc(e); err == nil && len(doc.Cues) > 0 {
		return doc, true
	}
	if doc, err := s.o.Lyr.ReadCache(e.ID); err == nil && len(doc.Cues) > 0 {
		return doc, true
	}
	if doc, err := s.subsCacheDoc(e); err == nil && len(doc.Cues) > 0 {
		return doc, true
	}
	return nil, false
}
