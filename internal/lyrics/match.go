package lyrics

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	Record       Record  `json:"record"`
	Variant      Variant `json:"variant"`
	TitleSim     float64 `json:"titleSim"`
	ArtistSim    float64 `json:"artistSim"`
	Score        float64 `json:"score"`
	Instrumental bool
	TitleOnly    bool   `json:"titleOnly"` // 只有歌名、没有艺人可对照
	AcceptBase   string `json:"-"`
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
		s = r
	}
	// 合唱/多人署名写着不一样："孙茜茹&马良" vs "马良"、"刘至佳 - 韩瞳" vs "刘至佳, 韩瞳"。
	// 只要其中某一位艺人对得上，就认为艺人这一关过了。
	if best := bestArtistToken(a, b); best > s {
		s = best
	}
	return s
}

// normArtistToken 抹掉「003L（桃籽）」这类噪音：括号、"003" 序号前缀。
// 酷狗把同一位歌手写成 "L（桃籽）"，本地标签写成 "003L 桃籽"，不归一化就对不上。
var (
	artistBracketRe = regexp.MustCompile(`[（()）\[\]【】]`)
	artistLeadNoRe  = regexp.MustCompile(`^\d{2,}\s*[-.、_ ]?\s*`)
)

func normArtistToken(s string) string {
	s = artistBracketRe.ReplaceAllString(s, " ")
	s = artistLeadNoRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

var artistSplitRe = regexp.MustCompile(`(?i)(?:\s*(?:&|/|,|，|、|;|\||x|vs\.?|feat\.?|ft\.?|with)\s*)|\s*[-—–]\s*`)

// bestArtistToken 把两边都按「多人分隔符」切开，取逐对比较里的最高分。
func bestArtistToken(a, b string) float64 {
	as := artistSplitRe.Split(a, -1)
	bs := artistSplitRe.Split(b, -1)
	best := 0.0
	for _, x := range as {
		xt := normArtistToken(x)
		if len([]rune(xt)) < 2 {
			continue
		}
		for _, y := range bs {
			yt := normArtistToken(y)
			if len([]rune(yt)) < 2 {
				continue
			}
			v := similarity(xt, yt)
			if o := runeOverlap(xt, yt); o > v {
				v = o
			}
			if v > best {
				best = v
			}
		}
	}
	return best
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
	// 候选里同时存在「A/B」和「B/A」时，某条记录两边都对得上就说明有歧义：
	// 单条记录（社区里只有一个上传）不足以判断谁是谁，容易选中网友上传的反序脏条目。
	reversed := map[string]bool{}
	for _, v := range variants {
		if v.Title == "" || v.Artist == "" {
			continue
		}
		reversed[normalizeKey(v.Artist)+"\x00"+normalizeKey(v.Title)] = true
	}
	// 首选朝向（候选队列第一条）就是「最像歌名/艺人」的拆法，它自己不需要靠条数证明。
	preferred := ""
	for _, v := range variants {
		if v.Title != "" && v.Artist != "" {
			preferred = normalizeKey(v.Title) + "\x00" + normalizeKey(v.Artist)
			break
		}
	}

	add := func(v Variant, recs []Record) {
		support := len(recs)
		ambiguous := reversed[normalizeKey(v.Title)+"\x00"+normalizeKey(v.Artist)]
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
			tSim := similarity(coreTitle(v.Title), coreTitle(r.TrackName))
			aSim := 0.0
			if v.Artist != "" {
				aSim = artistSimilarity(v.Artist, r.ArtistName)
			}
			if tSim < minTitleSim {
				continue
			}
			pair := normalizeKey(v.Title) + "\x00" + normalizeKey(v.Artist)
			if ambiguous && pair != preferred && support < 2 {
				continue // 非首选朝向、又只有一条上传 → 多半是网友反着写的那条，不敢认
			}
			if v.Artist != "" && aSim < minArtistSim {
				continue
			}
			score := tSim * 100
			if v.Artist != "" {
				score += aSim * 60
			}
			// 反过来的记录（track=艺人、artist=歌名）基本都是网友自己传的脏条目，
			// 歌名对得上但内容常常是另一首歌/另一版 —— 直接丢掉，不是扣分的问题。
			if v.Artist != "" && similarity(v.Title, r.ArtistName) >= 0.9 && similarity(v.Artist, r.TrackName) >= 0.9 {
				continue
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
					// DJ 版/加长版时长能差一倍；艺人已经对得很准时只扣分不否决
					if aSim >= 0.85 {
						score -= 8
					} else {
						score -= 20
					}
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

	// 候选按批并发查（LRCLIB 单次要 2~3 秒，串行会把批量缓存拖成几小时）。
	// 每批最多 3 个写法，批内并发；批与批之间看是否已经拿到硬命中。
	var lastErr error
	var mu sync.Mutex
	var cand []Variant
	for _, v := range variants {
		if strings.TrimSpace(v.Title) == "" {
			continue
		}
		cand = append(cand, v)
		if len(cand) >= 4 {
			break // 最多试 4 种写法，避免一首歌把歌词库打成筛子
		}
	}
	for start := 0; start < len(cand); start += 3 {
		end := start + 3
		if end > len(cand) {
			end = len(cand)
		}
		var wg sync.WaitGroup
		for _, v := range cand[start:end] {
			v := v
			wg.Add(1)
			go func() {
				defer wg.Done()
				recs, err := c.SearchTrack(ctx, v.Title, v.Artist)
				mu.Lock()
				if err != nil && !errors.Is(err, ErrNotFound) {
					lastErr = err // 网络/限流错误不致命：换一种写法继续试
				}
				mu.Unlock()
				if len(recs) == 0 {
					return
				}
				mu.Lock()
				add(v, recs)
				mu.Unlock()
				// 只在「搜到一点但不多」时补一次自由文本搜索；
				// 一条都没有说明这个写法本身不对，再查一遍纯属浪费请求。
				if len(recs) < 2 {
					if more, err := c.SearchQuery(ctx, strings.TrimSpace(v.Title+" "+v.Artist)); err == nil && len(more) > 0 {
						mu.Lock()
						add(v, more)
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		// 已经有「歌名+艺人都很强」的命中就不折腾后面的写法了
		if best := bestAccepted(accepts); best != nil && best.Score >= 150 {
			break
		}
	}

	sawInstrumental := false
	if len(accepts) == 0 {
		// 最后一招：歌名单独搜。翻唱/多人合唱的艺人写法经常对不上（"韩可可" 唱《错位时空》），
		// 但只要歌名几乎一模一样、时长也对得上，就认为歌词是同一首歌的。
		// 反过来，艺人给得明明白白却对不上、又拿不到时长核对时，不许借同名歌词，避免串歌。
		// LRCLIB 没有时按顺序补：酷狗（华语最全）→ 网易云（有时限流）→ 同名借词
		if m := c.bestKugou(ctx, variants, album, duration); m != nil {
			if m.Instrumental {
				// 酷狗说这是纯音乐：先记下结论，再给别的源一次机会
				sawInstrumental = true
			} else {
				return m, nil
			}
		}
		if m := c.bestNetease(ctx, variants, album, duration); m != nil {
			return m, nil
		}
		allowTitleOnly := duration > 0
		for _, v := range variants {
			if strings.TrimSpace(v.Artist) == "" {
				allowTitleOnly = true
				break
			}
		}
		if allowTitleOnly {
			if m := titleOnlyFallback(c, ctx, variants, duration); m != nil {
				return m, nil
			}
		}
		if sawInstrumental {
			return nil, ErrInstrumental
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrNotFound
	}
	sort.SliceStable(accepts, func(i, j int) bool { return accepts[i].Score > accepts[j].Score })
	top := accepts[0]
	return &top, nil
}

// titleOnlyFallback 是最后的兜底：只用歌名去搜，要求歌名极像 + 时长不冲突。
// 按候选顺序试（不按长度挑——"孙茜茹&马良 - 往后余生" 这种整条候选最长却最不像歌名）。
// 分数刻意压得很低，保证「有正常命中时不会抢戏」。
func titleOnlyFallback(c *Client, ctx context.Context, variants []Variant, duration float64) *Match {
	if !c.Enabled {
		return nil
	}
	tried := 0
	for _, v := range variants {
		t := strings.TrimSpace(v.Title)
		if t == "" || len([]rune(t)) < 2 || len([]rune(t)) > 24 {
			continue
		}
		if tried >= 1 { // 兜底只试一个候选，别把歌词库打成筛子
			break
		}
		tried++
		recs, err := c.SearchTrack(ctx, t, "")
		if err != nil {
			continue
		}
		var best *Match
		for _, r := range recs {
			if strings.TrimSpace(r.TrackName) == "" || (r.PlainLyrics == "" && r.SyncedLyrics == "") {
				continue
			}
			if noisyName(r.TrackName) || noisyName(r.ArtistName) {
				continue
			}
			tSim := similarity(coreTitle(t), coreTitle(r.TrackName))
			if tSim < 0.92 {
				continue
			}
			if best != nil && tSim < best.TitleSim {
				continue
			}
			// 借同名歌词必须站得住脚：要么艺人对得上（合唱/前缀差异），
			// 要么时长几乎一模一样（同一首歌的另一个上传）。否则宁可没有歌词，
			// 也不要把「无问」（邓伟标民乐）配成「无问」（毛不易）的歌词。
			aSim := artistSimilarity(v.Artist, r.ArtistName)
			dt := -1.0
			if duration > 0 && r.Duration > 0 {
				dt = duration - r.Duration
				if dt < 0 {
					dt = -dt
				}
			}
			if aSim < 0.45 {
				if r.Duration <= 0 || dt > 3 {
					continue
				}
			} else if dt > 20 {
				continue
			}
			best = &Match{
				Record:    r,
				Variant:   Variant{Title: t, Artist: v.Artist, Source: "title-only"},
				TitleSim:  tSim,
				ArtistSim: aSim,
				Score:     tSim*100 - 55,
				TitleOnly: true,
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
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
		// 已经判定过「没有歌词（纯音乐）」的，直接给结论，不再去打歌词库。
		if _, err := c.ReadNoLyric(key); err == nil {
			return InstrumentalDoc(firstTitle(variants), firstArtist(variants)), nil, nil
		}
		if doc, err := c.ReadCache(key); err == nil {
			doc.Source = "cache"
			return doc, nil, nil
		}
	}
	if !c.Enabled {
		return nil, nil, ErrNotFound
	}
	// 用户在候选列表里手动选了某条，即使之前标过纯音乐也要照办
	if pick > 0 {
		// 用户在候选列表里手动指定了某条：只在同一条路径里取它
		if doc, err := c.Resolve(ctx, key, variants[0].Title, variants[0].Artist, album, duration, force, pick); err == nil {
			return doc, nil, nil
		}
	}
	m, err := c.BestMatch(ctx, variants, album, duration)
	if errors.Is(err, ErrInstrumental) {
		// 歌词库确认是纯音乐：落一条结论（下次不再问），返回空歌词 + 说明。
		_ = c.WriteNoLyric(key, NoLyric{Reason: "instrumental", Source: "kugou", Note: NoLyricNote})
		return InstrumentalDoc(firstTitle(variants), firstArtist(variants)), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	doc := DocFromRecord(m.Record)
	if doc.Source == "" {
		doc.Source = "lrclib"
	}
	if err := c.WriteCache(key, doc); err != nil {
		debugLog("写歌词缓存失败: %v", err)
	}
	return doc, m, nil
}

func firstTitle(vs []Variant) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0].Title
}

func firstArtist(vs []Variant) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0].Artist
}

var coreBracketRe = regexp.MustCompile(`[（(\[][^）)\]|]{0,20}[）)\]|]`)

// coreTitle 去掉歌名里的括号附件（Live/DJ版/古琴名曲/10倍音质…），只留核心歌名，
// 避免同一个歌名因为版本后缀被算成两个不同的歌。
func coreTitle(s string) string {
	core := strings.TrimSpace(coreBracketRe.ReplaceAllString(s, " "))
	if core == "" {
		return strings.TrimSpace(s)
	}
	return core
}
