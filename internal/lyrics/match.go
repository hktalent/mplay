package lyrics

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Variant 是一种「歌名 + 艺人」猜测。文件名的写法五花八门，
// 与其在扫描阶段赌一个结果，不如把几种拆法都拿去歌词库问一遍，
// 由「歌名像不像 + 艺人像不像」来决定谁是真的。
type Variant struct {
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Source  string `json:"source,omitempty"`
	Library bool   `json:"library,omitempty"` // 来自曲库已有标签（更可信）
}

// Match 是一条被校验过的匹配结果。
type Match struct {
	Record     Record  `json:"record"`
	Variant    Variant `json:"variant"`
	TitleSim   float64 `json:"titleSim"`
	ArtistSim  float64 `json:"artistSim"`
	Score      float64 `json:"score"`
	TitleOnly  bool    `json:"titleOnly"` // 只有歌名、没有艺人可对照
	AcceptBase string  `json:"-"`
}

// 阈值：歌名必须足够像，带了艺人的话艺人也要对得上。
// 宁可漏一条，也不要给用户一首「别的版本/别的歌」的歌词。
const (
	minTitleSim  = 0.75
	minArtistSim = 0.55
)

// artistSimilarity 在普通相似度之上补一个字符级重合度：
// 同一个人的写法经常不一样（G.E.M.邓紫棋 / G.E.M. 鄧紫棋、Eason Chan / 陈奕迅），
// 而「赵雷」和「陳一發兒」这种完全不同的名字重合度为 0，照样会被挡住。
func artistSimilarity(a, b string) float64 {
	s := similarity(a, b)
	r := runeOverlap(a, b)
	if r > s {
		return r
	}
	return s
}

func runeOverlap(a, b string) float64 {
	x := []rune(normalizeKey(a))
	y := []rune(normalizeKey(b))
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	set := map[rune]int{}
	for _, r := range x {
		set[r]++
	}
	inter := 0
	for _, r := range y {
		if set[r] > 0 {
			set[r]--
			inter++
		}
	}
	longer := len(x)
	if len(y) > longer {
		longer = len(y)
	}
	return float64(inter) / float64(longer)
}

// BestMatch 在多个候选写法里挑出最可信的一条歌词库记录。
// 只返回「歌名像且艺人像」的结果；一条都不合格就返回 ErrNotFound。
func (c *Client) BestMatch(ctx context.Context, variants []Variant, album string, duration float64) (*Match, error) {
	if !c.Enabled {
		return nil, ErrNotFound
	}
	seen := map[int64]bool{}
	var accepts []Match

	add := func(v Variant, recs []Record) {
		support := len(recs)
		for _, r := range recs {
			if strings.TrimSpace(r.TrackName) == "" {
				continue
			}
			if r.PlainLyrics == "" && r.SyncedLyrics == "" {
				continue // 空的记录没意义
			}
			if r.ID != 0 {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
			}
			tSim := similarity(v.Title, r.TrackName)
			aSim := 0.0
			if v.Artist != "" {
				aSim = artistSimilarity(v.Artist, r.ArtistName)
			}
			if tSim < minTitleSim {
				continue
			}
			if v.Artist != "" && aSim < minArtistSim {
				continue
			}
			score := tSim * 100
			if v.Artist != "" {
				score += aSim * 60
			}
			// 反过来的记录（track=艺人、artist=歌名）基本都是网友自己传的脏条目，
			// 歌名对得上但内容常常是另一首歌/另一版。
			if v.Artist != "" && similarity(v.Title, r.ArtistName) >= 0.9 && similarity(v.Artist, r.TrackName) >= 0.9 {
				score -= 25
			}
			// 歌名/艺人里还挂着 "MP3_160K"、"320K" 这种水印，多半不是原始条目
			if noisyName(r.TrackName) || noisyName(r.ArtistName) {
				score -= 30
			}
			// 同样一种写法能搜出一大把记录，说明这个方向是社区通用的写法
			if support >= 5 {
				score += 12
			}
			if album != "" {
				score += similarity(album, r.AlbumName) * 12
			}
			if r.HasSynced() {
				score += 6
			}
			if duration > 0 && r.Duration > 0 {
				d := duration - r.Duration
				if d < 0 {
					d = -d
				}
				switch {
				case d <= 2:
					score += 12
				case d <= 6:
					score += 6
				case d > 20:
					score -= 12
				}
			}
			if v.Library {
				score += 4
			}
			accepts = append(accepts, Match{
				Record:    r,
				Variant:   v,
				TitleSim:  tSim,
				ArtistSim: aSim,
				Score:     score,
				TitleOnly: v.Artist == "",
			})
		}
	}

	var lastErr error
	for i, v := range variants {
		if strings.TrimSpace(v.Title) == "" {
			continue
		}
		if i >= 5 {
			break // 最多试 5 种写法，避免一首歌把歌词库打成筛子
		}
		recs, err := c.SearchTrack(ctx, v.Title, v.Artist)
		if err != nil && !errors.Is(err, ErrNotFound) {
			lastErr = err // 网络/限流错误不致命：换一种写法继续试
			continue
		}
		add(v, recs)
		if len(recs) < 2 {
			if more, err := c.SearchQuery(ctx, strings.TrimSpace(v.Title+" "+v.Artist)); err == nil {
				add(v, more)
			}
		}
		// 已经有「歌名+艺人都很强」的命中就不折腾后面的写法了
		if best := bestAccepted(accepts); best != nil && best.Score >= 150 {
			break
		}
	}
	if len(accepts) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrNotFound
	}
	sort.SliceStable(accepts, func(i, j int) bool { return accepts[i].Score > accepts[j].Score })
	top := accepts[0]
	return &top, nil
}

// noisyName 判断一条记录的名字里是否还带着资源站/码率水印。
func noisyName(s string) bool {
	low := strings.ToLower(s)
	for _, t := range []string{"mp3", "320k", "160k", "192k", "128k", "flac", "hq", "hd", "4k", "1080p", "_160", "完整版", "无损", "试听"} {
		if strings.Contains(low, t) {
			return true
		}
	}
	return false
}

func bestAccepted(ms []Match) *Match {
	if len(ms) == 0 {
		return nil
	}
	best := ms[0]
	for _, m := range ms[1:] {
		if m.Score > best.Score {
			best = m
		}
	}
	return &best
}

// ResolveBest 是「按多种写法去找歌词」的入口：缓存 → BestMatch → 写缓存。
// 返回的第二个值是匹配到的原始记录（调用方可以据此修正曲库里的歌名/艺人）。
func (c *Client) ResolveBest(ctx context.Context, key string, variants []Variant, album string, duration float64, force bool, pick int64) (*Doc, *Match, error) {
	if !force && pick == 0 {
		if doc, err := c.ReadCache(key); err == nil {
			doc.Source = "cache"
			return doc, nil, nil
		}
	}
	if !c.Enabled {
		return nil, nil, ErrNotFound
	}
	if pick > 0 {
		// 用户在候选列表里手动指定了某条：只在同一条路径里取它
		if doc, err := c.Resolve(ctx, key, variants[0].Title, variants[0].Artist, album, duration, force, pick); err == nil {
			return doc, nil, nil
		}
	}
	m, err := c.BestMatch(ctx, variants, album, duration)
	if err != nil {
		return nil, nil, err
	}
	doc := DocFromRecord(m.Record)
	doc.Source = "lrclib"
	if err := c.WriteCache(key, doc); err != nil {
		debugLog("写歌词缓存失败: %v", err)
	}
	return doc, m, nil
}
