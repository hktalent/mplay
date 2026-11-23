package lyrics

import (
	"sort"
	"strings"
	"sync"
)

// Snippet 是歌词索引里的一行。
type Snippet struct {
	MediaID string  `json:"mediaId"`
	Title   string  `json:"title"`
	Artist  string  `json:"artist"`
	Kind    string  `json:"kind"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Trans   string  `json:"trans,omitempty"`
}

// SnippetHit 是一条命中的歌词行。
type SnippetHit struct {
	Snippet
	Score float64 `json:"score"`
}

// SnippetIndex 是「歌词片段 → 歌」的本地全文索引。
//
// 为什么本地建索引：LRCLIB 的 /api/search?q= 只匹配歌名/歌手（实测：搜一句英文歌词
// 只会返回名字恰好相同的歌），没有全文检索能力。要让「输入一句歌词找到歌、并且
// 直接跳到那句的时间点」真正可用，就必须对曲库自己的歌词（含已缓存的远端歌词）
// 建立索引。索引只在内存里，曲库变化或拉到新歌词时增量更新。
type SnippetIndex struct {
	mu    sync.RWMutex
	items []Snippet
	built int64
	count int
}

// NewSnippetIndex 创建索引。
func NewSnippetIndex() *SnippetIndex { return &SnippetIndex{} }

// Replace 全量替换索引（曲库扫描后调用）。
func (ix *SnippetIndex) Replace(items []Snippet, builtAt int64) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	ix.items = items
	ix.built = builtAt
	ix.count = len(items)
	ix.mu.Unlock()
}

// BuiltAt 返回索引构建时间戳。
func (ix *SnippetIndex) BuiltAt() int64 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.built
}

// Count 返回索引行数。
func (ix *SnippetIndex) Count() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.items)
}

// AddDoc 把某首歌的歌词增量加入索引（拉取到远端歌词后调用）。
func (ix *SnippetIndex) AddDoc(mediaID, title, artist, kind string, doc *Doc) {
	if ix == nil || doc == nil || len(doc.Cues) == 0 {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	// 先删旧的，再插新的
	out := ix.items[:0]
	for _, it := range ix.items {
		if it.MediaID != mediaID {
			out = append(out, it)
		}
	}
	ix.items = out
	for _, c := range doc.Cues {
		ix.items = append(ix.items, Snippet{
			MediaID: mediaID, Title: title, Artist: artist, Kind: kind,
			Start: c.Start, End: c.End, Text: c.Text, Trans: c.Trans,
		})
	}
}

// Search 返回命中的歌词行；同一首歌最多保留 3 行，按匹配质量排序。
func (ix *SnippetIndex) Search(q string, limit int) []SnippetHit {
	if ix == nil {
		return nil
	}
	nq := NormalizeLoose(q)
	if nq == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	ix.mu.RLock()
	items := ix.items
	ix.mu.RUnlock()

	var hits []SnippetHit
	perMedia := map[string]int{}
	for _, it := range items {
		norm := NormalizeLoose(it.Text)
		if !strings.Contains(norm, nq) {
			continue
		}
		if perMedia[it.MediaID] >= 3 {
			continue
		}
		score := 40 + 60*float64(len([]rune(nq))/max(1, len([]rune(norm))))
		if strings.HasPrefix(norm, nq) {
			score += 15
		}
		perMedia[it.MediaID]++
		hits = append(hits, SnippetHit{Snippet: it, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Start < hits[j].Start
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// NormalizeLoose 用于歌词全文匹配：只保留文字/数字，去掉标点与空格。
func NormalizeLoose(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r = r - 0xFF01 + 0x21
		case r == 0x3000:
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			(r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) ||
			(r >= 0x3040 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7AF) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
