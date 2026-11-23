package media

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Guess 是「从文件名/标签里拆出来的一种可能的 歌名+艺人」。
// 同一个文件名往往能拆出好几种结果（谁在前谁在后、要不要把空格当分隔符），
// 所以这里不做"猜一个就定死"，而是给出一条排序过的候选队列，
// 交给歌词库校验去判真假。
type Guess struct {
	Title  string
	Artist string
	Source string // tag / bracket / filename / filename-rev / split-last / split-first
}

// 关键：括号里出现这些词，说明它只是资源站的噪音，整段丢掉。
var junkSegments = []string{
	"320k", "128k", "192k", "256k", "64k", "flac", "ape", "wav", "hires", "hi-res", "hq", "hd",
	"mp3", "m4a", "aac", "ogg", "mp4", "mkv",
	"无损", "高清", "超清", "标清", "高音质", "原声", "原版", "正式版", "完整版", "官方版",
	"伴奏", "纯音乐", "铃声", "试听", "下载", "免费", "更多", "关注", "公众号",
	"中文字幕", "中英双语", "双语字幕", "字幕", "片段", "剪辑",
	"live", "remix", "dj", "mv", "official", "audio", "lyrics", "instrumental",
	"4k", "1080p", "720p", "hdr", "demo",
}

// 域名/资源站特征：整段括号或整条前后缀直接丢掉。
var siteTokens = []string{
	"www.", "http", "://", ".com", ".cn", ".net", ".org", ".top", ".xyz", ".vip", "music", "mp3",
	"音乐网", "下载站", "资源站", "音悦台", "酷狗", "酷我", "网易云", "qq音乐", "咪咕", "虾米", "多米", "一听", "5nd", "9ku",
}

// 不带括号、直接用 -/_/空格 当分隔挂在名字上的噪音（"陈奕迅_十年_320k"）。
var junkTokenRe = regexp.MustCompile(`(?i)(^|[\s\-_—–\|、,，])(320k|192k|128k|256k|64k|flac|ape|wav|hires|hi-?res|hq|hd|4k|1080p|720p|mp3|m4a|aac|ogg|无损|高清|超清|标清|高音质|完整版|官方版|正式版|原版|伴奏|纯音乐|铃声|试听|下载|免费|mv|remix|dj版)([\s\-_—–\|、,，.。]|$)`)

var (
	bracketRe   = regexp.MustCompile(`[\[\(（【{<][^\]\)）】}>]*[\]\)）】}>]`)
	leadNoRe    = regexp.MustCompile(`^\s*(?:[\(\[]?\d{1,3}[\)\]]?\s*[\.、\-\_\)）]\s*|\d{1,3}\s+)`)
	multiSpace  = regexp.MustCompile(`[\t ]{2,}`)
	urlHeadRe   = regexp.MustCompile(`(?i)^\s*[\[\{\(【]?(?:www\.|http)[^\]\}\)】]*[\]\}\)】]?\s*[-_— ]*`)
	trailingTag = regexp.MustCompile(`(?i)\s*[-_]\s*(?:mp3|flac|ape|wav|m4a)\s*$`)
	// feat 前面必须是词边界，否则 "Taylor Swift" 里的 "ft" 会被吃掉。
	featRe      = regexp.MustCompile(`(?i)(^|[\s\-_\(（\[【])[\[\(（【]?(?:feat\.?|ft\.?)\s+[^\s\-—–\)）\]】]*`)
	bookTitleRe = regexp.MustCompile(`《([^》]+)》`)
)

// CleanName 去掉资源站前缀、码率/音质呐喊、序号、扩展名，只留「艺人 - 歌名」这类骨架。
func CleanName(name string) string {
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = urlHeadRe.ReplaceAllString(base, "")
	// 括号分段：噪音整段删，非噪音保留内容（"蓝莲花(无损)" 要留 "蓝莲花"，"倔强 (Live)" 留 "倔强"）
	base = bracketRe.ReplaceAllStringFunc(base, func(seg string) string {
		low := strings.ToLower(seg)
		for _, t := range append(append([]string{}, junkSegments...), siteTokens...) {
			if strings.Contains(low, strings.ToLower(t)) {
				return " "
			}
		}
		// 纯数字/符号的括号也丢（"(1)"、"(2023)" 这类序号/年份不可靠时不参与命名）
		inner := strings.TrimSpace(strings.Trim(seg, "[]()（）【】{}<>"))
		if inner == "" || allDigits(inner) {
			return " "
		}
		return " " + inner + " "
	})
	base = trailingTag.ReplaceAllString(base, "")
	for i := 0; i < 3; i++ {
		nb := junkTokenRe.ReplaceAllString(base, " ")
		if nb == base {
			break
		}
		base = nb
	}
	base = leadNoRe.ReplaceAllString(base, "")
	base = featRe.ReplaceAllString(base, "$1 ")
	base = strings.ReplaceAll(base, "_", " - ")
	base = strings.ReplaceAll(base, "\u00a0", " ")
	base = multiSpace.ReplaceAllString(base, " ")
	base = strings.TrimSpace(base)
	base = strings.Trim(base, "-—–_|｜、,，.。 ")
	base = multiSpace.ReplaceAllString(base, " ")
	return strings.TrimSpace(base)
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// looksLikeArtist 判断某一段更像艺人而不是歌名。
// 艺人名通常短、不含数字/书名号/问号感叹号，也不像「某某完整版」。
func looksLikeArtist(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return -10
	}
	n := len([]rune(s))
	if n > 22 {
		return -6
	}
	score := 1.0
	if n <= 12 {
		score += 0.6
	}
	if n <= 6 {
		score += 0.4
	}
	hasHan := hasCJK(s)
	switch {
	case hasHan && n >= 3 && n <= 6:
		score += 0.3 // 周杰伦 / 逃跑计划 / 刘若英 这种最常见的艺名长度
	case hasHan && n >= 8:
		score -= 0.3 // 「夜空中最亮的星」这类长句基本都是歌名
	}
	if strings.Contains(s, ".") && n <= 12 {
		score += 0.2 // S.H.E / G.E.M. 这种缩写字艺人
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			score -= 1.2
		}
	}
	for _, mark := range []string{"《", "》", "?", "？", "!", "！", ",", "，", "。", "、"} {
		if strings.Contains(s, mark) {
			score -= 1.0
		}
	}
	low := strings.ToLower(s)
	for _, t := range junkSegments {
		if strings.Contains(low, t) {
			score -= 1.5
		}
	}
	if strings.Contains(low, " feat") {
		score -= 1.0
	}
	return score
}

var separators = []string{" - ", " — ", " – ", " – ", " - ", "－", " – ", " | ", "｜", "、", " _ ", "_-", "_"}

// splitPairs 把清理后的名字按各种分隔符拆成「左右两段」。
func splitPairs(clean string) [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	add := func(l, r string) {
		l, r = strings.TrimSpace(l), strings.TrimSpace(r)
		l = strings.Trim(l, "-—–_|｜、,，.。 ")
		r = strings.Trim(r, "-—–_|｜、,，.。 ")
		if l == "" || r == "" {
			return
		}
		k := strings.ToLower(l + "\x00" + r)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, [2]string{l, r})
	}
	for _, sep := range separators {
		if !strings.Contains(clean, sep) {
			continue
		}
		parts := strings.Split(clean, sep)
		if len(parts) == 2 {
			add(parts[0], parts[1])
			continue
		}
		if len(parts) > 2 {
			// "艺人 - 歌名 - 专辑" 这种：第一段与最后两段各试一次
			add(parts[0], strings.Join(parts[1:], sep))
			add(strings.Join(parts[:len(parts)-1], sep), parts[len(parts)-1])
		}
	}
	// 裸连字符（周杰伦-稻香、04.王菲-红豆）：只在两侧都有中日韩文字时拆，
	// 否则 "Taylor-Swift" 这种会被拆坏。
	if len(out) == 0 {
		if i := strings.Index(clean, "-"); i > 0 && i < len(clean)-1 {
			l, r := clean[:i], clean[i+1:]
			if hasCJK(l) && hasCJK(r) {
				add(l, r)
			}
		}
	}
	// 没有明显分隔符时，用空格切分：最后一段当艺人 / 第一段当艺人
	if len(out) == 0 {
		fields := strings.Fields(clean)
		if len(fields) >= 3 {
			add(strings.Join(fields[:len(fields)-1], " "), fields[len(fields)-1])
			add(fields[0], strings.Join(fields[1:], " "))
		} else if len(fields) == 2 {
			add(fields[0], fields[1])
		}
	}
	return out
}

// GuessNames 从文件名生成排好序的候选。排序规则：
// 艺人味更浓的一段当艺人；先给一组，再给反序，让歌词库校验来裁决。
func GuessNames(name string) []Guess {
	clean := CleanName(name)
	if clean == "" {
		return nil
	}
	var gs []Guess

	// 《歌名》最可信：书名号里的就是歌名，前后剩下的当艺人
	if m := bookTitleRe.FindStringSubmatch(clean); len(m) == 2 {
		title := strings.TrimSpace(m[1])
		rest := strings.TrimSpace(strings.Replace(clean, m[0], " ", 1))
		rest = strings.Trim(rest, "-—–_|｜、,，.。 ()（）")
		if title != "" {
			gs = append(gs, Guess{Title: title, Artist: rest, Source: "bracket"})
			if rest != "" {
				gs = append(gs, Guess{Title: rest, Artist: title, Source: "bracket-rev"})
			}
		}
	}

	for _, p := range splitPairs(clean) {
		lA := looksLikeArtist(p[0])
		rA := looksLikeArtist(p[1])
		if lA >= rA {
			gs = append(gs, Guess{Title: p[1], Artist: p[0], Source: "filename"})
			gs = append(gs, Guess{Title: p[0], Artist: p[1], Source: "filename-rev"})
		} else {
			gs = append(gs, Guess{Title: p[0], Artist: p[1], Source: "filename"})
			gs = append(gs, Guess{Title: p[1], Artist: p[0], Source: "filename-rev"})
		}
	}

	// 兜底：整条当歌名（"周深 大鱼" 这类只有空格没有分隔符的最常见场景）
	gs = append(gs, Guess{Title: clean, Source: "filename-whole"})
	if fields := strings.Fields(clean); len(fields) >= 2 {
		// 两段式里头尾各试一次当艺人
		gs = append(gs, Guess{Title: strings.Join(fields[1:], " "), Artist: fields[0], Source: "split-first"})
		gs = append(gs, Guess{Title: strings.Join(fields[:len(fields)-1], " "), Artist: fields[len(fields)-1], Source: "split-last"})
	}

	// 去重（按规范化后的 歌名+艺人）
	seen := map[string]bool{}
	var out []Guess
	for _, g := range gs {
		g.Title = strings.TrimSpace(g.Title)
		g.Artist = strings.TrimSpace(strings.Trim(g.Artist, "-—–_|｜、,，.。 "))
		if g.Title == "" {
			continue
		}
		k := NormalizeKey(g.Title) + "\x00" + NormalizeKey(g.Artist)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, g)
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// GuessWithTags 优先用内嵌标签，标签不全时补文件名候选。
// tags 里只有歌名没艺人时，把文件名里最像艺人的一段补上。
func GuessWithTags(name, tagTitle, tagArtist string) []Guess {
	tagTitle = strings.TrimSpace(tagTitle)
	tagArtist = strings.TrimSpace(tagArtist)
	var out []Guess
	if tagTitle != "" && tagArtist != "" {
		return []Guess{{Title: tagTitle, Artist: tagArtist, Source: "tag"}}
	}
	nameGuesses := GuessNames(name)
	if tagTitle != "" {
		// 标签给了歌名，艺人从文件名的「非歌名段」里找
		best := ""
		for _, g := range nameGuesses {
			if NormalizeKey(g.Title) == NormalizeKey(tagTitle) && g.Artist != "" {
				best = g.Artist
				break
			}
		}
		out = append(out, Guess{Title: tagTitle, Artist: best, Source: "tag"})
		for _, g := range nameGuesses {
			if NormalizeKey(g.Title) != NormalizeKey(tagTitle) {
				out = append(out, g)
			}
		}
		return out
	}
	if tagArtist != "" {
		// 标签给了艺人但没歌名：文件名每一段都当歌名配这个艺人试一遍
		for _, g := range nameGuesses {
			out = append(out, Guess{Title: g.Title, Artist: tagArtist, Source: "tag-artist"})
		}
	}
	if len(out) > 0 && len(out) <= 10 {
		return out
	}
	if len(out) > 10 {
		return out[:10]
	}
	return nameGuesses
}
