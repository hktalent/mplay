// Package lyrics 把各种歌词/字幕格式解析成同一个 cue 模型，供前端与 API 共用。
//
// 统一模型的好处：桌面端、web 端、视频字幕叠层只需要一套同步逻辑；
// 服务端也能在拉取远端歌词时立刻判断「有没有时间轴」。
package lyrics

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Word 是逐字时间戳（增强型 LRC / karaoke）。
type Word struct {
	T float64 `json:"t"`
	W string  `json:"w"`
}

// Cue 是屏幕上的一行：原文 + 可选译文 + 可选逐字时间。
type Cue struct {
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	Text  string  `json:"t"`
	Trans string  `json:"tr,omitempty"`
	Words []Word  `json:"words,omitempty"`
}

// Doc 是一份可播放的歌词/字幕。
type Doc struct {
	Format   string            `json:"format"`
	Synced   bool              `json:"synced"`
	Cues     []Cue             `json:"cues"`
	Meta     map[string]string `json:"meta,omitempty"`
	Source   string            `json:"source,omitempty"` // sidecar / tag / cache / lrclib / manual
	Title    string            `json:"title,omitempty"`
	Artist   string            `json:"artist,omitempty"`
	Album    string            `json:"album,omitempty"`
	Duration float64           `json:"duration,omitempty"`
	Offset   float64           `json:"offset,omitempty"`
	LRCLIBID int64             `json:"lrclibId,omitempty"`
	// Instrumental 为真表示「歌词库确认这首歌没有词（纯音乐）」，
	// 此时 Cues 为空，Note 里是给用户看的说法。
	Instrumental bool   `json:"instrumental,omitempty"`
	Note         string `json:"note,omitempty"`
}

var (
	lrcMeta  = regexp.MustCompile(`^\s*\[([a-zA-Z][a-zA-Z0-9_]{0,15}):([^\]]*)\]\s*$`)
	lrcTime  = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?::(\d{1,2}))?(?:[.,](\d{1,3}))?\]`)
	wordTime = regexp.MustCompile(`<(\d{1,3}):(\d{1,2})(?::(\d{1,2}))?(?:[.,](\d{1,3}))?>`)
	srtTime  = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})`)
	assTime  = regexp.MustCompile(`^\s*(\d+):(\d{1,2}):(\d{1,2}(?:[.,]\d{1,3})?)\s*$`)
	htmlTag  = regexp.MustCompile(`<[^>]+>`)
	assTag   = regexp.MustCompile(`\{[^}]*\}`)
)

// Detect 在没有扩展名时按内容猜格式。
func Detect(text string) string {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "WEBVTT"):
		return "vtt"
	case strings.Contains(t, "[Events]") && strings.Contains(t, "Dialogue:"):
		return "ass"
	case srtTime.MatchString(t) && !strings.Contains(t, "[") && !strings.HasPrefix(t, "WEBVTT"):
		return "srt"
	case lrcTime.MatchString(t):
		return "lrc"
	}
	return "txt"
}

// Parse 按扩展名/内容解析。
func Parse(data []byte, ext string) (*Doc, error) {
	text := DecodeText(data)
	ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	if ext == "" {
		ext = Detect(text)
	}
	switch ext {
	case "lrc", "elrc", "txt", "srt", "vtt", "ass", "ssa", "sub":
		// sub 是微DVD图形字幕，这里按 srt 文本尝试
	default:
		ext = Detect(text)
	}
	var doc *Doc
	switch ext {
	case "lrc", "elrc":
		doc = ParseLRC(text)
	case "srt", "sub":
		doc = ParseSRT(text)
	case "vtt":
		doc = ParseVTT(text)
	case "ass", "ssa":
		doc = ParseASS(text)
	default:
		doc = ParsePlain(text)
	}
	if doc == nil || len(doc.Cues) == 0 {
		return nil, errors.New("没有解析出任何歌词行")
	}
	doc.Format = ext
	if doc.Synced {
		finishTimings(doc)
	}
	return doc, nil
}

// ParseLRC 解析 [mm:ss.xx] 以及增强型逐字 <mm:ss.xx>。
func ParseLRC(text string) *Doc {
	doc := &Doc{Format: "lrc", Meta: map[string]string{}, Synced: false}
	offsetMs := 0
	plain := []string{}

	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if m := lrcMeta.FindStringSubmatch(line); m != nil {
			key := strings.ToLower(m[1])
			val := strings.TrimSpace(m[2])
			doc.Meta[key] = val
			if key == "offset" {
				if v, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					offsetMs = v
				}
			}
			continue
		}

		locs := lrcTime.FindAllStringSubmatchIndex(line, -1)
		if len(locs) == 0 {
			plain = append(plain, CleanLine(line))
			continue
		}
		// 文本 = 去掉所有时间标签后的部分
		body := strings.TrimSpace(lrcTime.ReplaceAllString(line, ""))
		words := parseWords(body)
		body = wordTime.ReplaceAllString(body, "")
		body = CleanLine(body)

		for _, m := range lrcTime.FindAllString(line, -1) {
			sub := lrcTime.FindStringSubmatch(m)
			start, _ := lrcTagTime(sub)
			doc.Cues = append(doc.Cues, Cue{Start: start, Text: body, Words: words})
		}
	}

	if len(doc.Cues) > 0 {
		doc.Synced = true
	} else {
		for i, l := range plain {
			doc.Cues = append(doc.Cues, Cue{Start: float64(i), End: float64(i + 1), Text: l})
		}
		doc.Synced = false
	}

	if offsetMs != 0 {
		doc.Offset = float64(offsetMs) / 1000
		shiftCues(doc.Cues, doc.Offset)
	}
	if doc.Synced {
		finishTimings(doc)
	} else {
		mergeTrans(doc)
	}
	return doc
}

func parseWords(s string) []Word {
	locs := wordTime.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return nil
	}
	tags := wordTime.FindAllString(s, -1)
	var out []Word
	for i, tag := range tags {
		// 该标签之后、下一个标签之前的文本就是这个词
		startAt := locs[i][1]
		endAt := len(s)
		if i+1 < len(locs) {
			endAt = locs[i+1][0]
		}
		text := strings.TrimSpace(stripWordTags(s[startAt:endAt]))
		if text == "" {
			continue
		}
		sub := wordTime.FindStringSubmatch(tag)
		t, _ := lrcTagTime(sub)
		out = append(out, Word{T: t, W: text})
	}
	return out
}

func stripWordTags(s string) string { return CleanLine(s) }

func lrcTagTime(m []string) (float64, error) {
	if len(m) < 3 {
		return 0, errors.New("bad tag")
	}
	min, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, err
	}
	sec, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, err
	}
	t := float64(min*60 + sec)
	if len(m) > 3 && m[3] != "" {
		sub, _ := strconv.Atoi(m[3])
		t += float64(sub) / 100
	}
	if len(m) > 4 && m[4] != "" {
		frac := m[4]
		v, _ := strconv.ParseFloat("0."+frac, 64)
		t += v
	}
	return t, nil
}

// ParseSRT 解析 SRT。
func ParseSRT(text string) *Doc {
	doc := &Doc{Format: "srt", Synced: true, Meta: map[string]string{}}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	blocks := regexp.MustCompile(`\n{2,}`).Split(strings.TrimSpace(text), -1)
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			continue
		}
		ti := -1
		for i, l := range lines {
			if srtTime.MatchString(l) {
				ti = i
				break
			}
		}
		if ti < 0 {
			continue
		}
		m := srtTime.FindStringSubmatch(lines[ti])
		start := hms(m[1], m[2], m[3], m[4])
		end := hms(m[5], m[6], m[7], m[8])
		body := strings.Join(lines[ti+1:], "\n")
		body = htmlTag.ReplaceAllString(body, "")
		cue := Cue{Start: start, End: end, Text: strings.TrimSpace(body)}
		splitBilingual(&cue)
		doc.Cues = append(doc.Cues, cue)
	}
	if len(doc.Cues) == 0 {
		doc.Synced = false
	}
	return doc
}

func hms(h, m, s, ms string) float64 {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	msv, _ := strconv.Atoi(strings.TrimSpace(ms + strings.Repeat("0", 3-len(ms))))
	return float64(hh*3600+mm*60+ss) + float64(msv)/1000
}

// ParseVTT 解析 WebVTT。
func ParseVTT(text string) *Doc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = regexp.MustCompile(`(?m)^WEBVTT.*$`).ReplaceAllString(text, "")
	doc := &Doc{Format: "vtt", Synced: true, Meta: map[string]string{}}
	blocks := regexp.MustCompile(`\n{2,}`).Split(strings.TrimSpace(text), -1)
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		ti := -1
		for i, l := range lines {
			if strings.Contains(l, "-->") {
				ti = i
				break
			}
		}
		if ti < 0 {
			continue
		}
		m := srtTime.FindStringSubmatch(lines[ti])
		if m == nil {
			continue
		}
		body := strings.Join(lines[ti+1:], "\n")
		body = htmlTag.ReplaceAllString(body, "")
		body = stripVTTVoice(body)
		cue := Cue{Start: hms(m[1], m[2], m[3], m[4]), End: hms(m[5], m[6], m[7], m[8]), Text: strings.TrimSpace(body)}
		splitBilingual(&cue)
		doc.Cues = append(doc.Cues, cue)
	}
	if len(doc.Cues) == 0 {
		doc.Synced = false
	}
	return doc
}

func stripVTTVoice(s string) string {
	// <v Speaker>text → text
	s = regexp.MustCompile(`<v[^>]*>`).ReplaceAllString(s, "")
	return s
}

// ParseASS 解析 ASS/SSA 的 [Events] 段。
func ParseASS(text string) *Doc {
	doc := &Doc{Format: "ass", Synced: true, Meta: map[string]string{}}
	inEvents := false
	format := []string{"Layer", "Start", "End", "Style", "Name", "MarginL", "MarginR", "MarginV", "Effect", "Text"}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(strings.ToLower(line), "[events]"):
			inEvents = true
			continue
		case strings.HasPrefix(line, "["):
			inEvents = false
		}
		if !inEvents {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "format:") {
			parts := strings.Split(line[len("format:"):], ",")
			format = format[:0]
			for _, p := range parts {
				format = append(format, strings.TrimSpace(p))
			}
			continue
		}
		if !strings.HasPrefix(strings.ToLower(line), "dialogue:") {
			continue
		}
		fields := strings.SplitN(line[len("dialogue:"):], ",", len(format))
		if len(fields) < len(format) {
			continue
		}
		vals := map[string]string{}
		for i, name := range format {
			vals[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(fields[i])
		}
		st := parseASSTime(vals["start"])
		et := parseASSTime(vals["end"])
		body := vals["text"]
		body = assTag.ReplaceAllString(body, "")
		body = strings.ReplaceAll(body, `\N`, "\n")
		body = strings.ReplaceAll(body, `\n`, "\n")
		body = strings.ReplaceAll(body, `\h`, " ")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		cue := Cue{Start: st, End: et, Text: body}
		splitBilingual(&cue)
		doc.Cues = append(doc.Cues, cue)
	}
	if len(doc.Cues) == 0 {
		doc.Synced = false
	}
	return doc
}

func parseASSTime(s string) float64 {
	m := assTime.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	sec, _ := strconv.ParseFloat(strings.ReplaceAll(m[3], ",", "."), 64)
	return float64(h*3600+mi*60) + sec
}

// ParsePlain 是纯文本歌词：没有时间轴，只能整页显示。
func ParsePlain(text string) *Doc {
	doc := &Doc{Format: "txt", Synced: false, Meta: map[string]string{}}
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = CleanLine(l)
		if l == "" {
			continue
		}
		doc.Cues = append(doc.Cues, Cue{Start: 0, End: 0, Text: l})
	}
	return doc
}

// splitBilingual：一行里同时有中日韩文字和拉丁字母时，拆成原文 + 译文两行。
func splitBilingual(c *Cue) {
	lines := strings.Split(c.Text, "\n")
	if len(lines) < 2 {
		return
	}
	cjkIdx, latinIdx := -1, -1
	for i, l := range lines {
		if hasCJK(l) {
			if cjkIdx < 0 {
				cjkIdx = i
			}
		} else if hasLatin(l) {
			if latinIdx < 0 {
				latinIdx = i
			}
		}
	}
	if cjkIdx >= 0 && latinIdx >= 0 && cjkIdx != latinIdx {
		c.Text = strings.TrimSpace(lines[cjkIdx])
		c.Trans = strings.TrimSpace(lines[latinIdx])
		// 其余行拼回原文
		for i, l := range lines {
			if i == cjkIdx || i == latinIdx {
				continue
			}
			if s := strings.TrimSpace(l); s != "" {
				c.Text += " " + s
			}
		}
		return
	}
	c.Text = strings.Join(strings.Split(c.Text, "\n"), " ")
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFF01 && r <= 0xFF60 {
			return true
		}
	}
	return false
}

func hasLatin(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// CleanLine 去掉 ASS 覆盖标签、HTML 标签，压缩空白。
func CleanLine(s string) string {
	s = assTag.ReplaceAllString(s, "")
	s = htmlTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// finishTimings 排序、补 End、合并同时间戳的双语行。
func finishTimings(doc *Doc) {
	// 丢掉纯空行（很多 LRC 用空行表示停顿，留着会让高亮闪一下）
	kept := doc.Cues[:0]
	for _, c := range doc.Cues {
		if strings.TrimSpace(c.Text) == "" && len(c.Words) == 0 {
			continue
		}
		kept = append(kept, c)
	}
	doc.Cues = kept

	sort.SliceStable(doc.Cues, func(i, j int) bool { return doc.Cues[i].Start < doc.Cues[j].Start })
	for i := range doc.Cues {
		if doc.Cues[i].End > doc.Cues[i].Start {
			continue
		}
		if i+1 < len(doc.Cues) {
			doc.Cues[i].End = doc.Cues[i+1].Start
		} else {
			doc.Cues[i].End = doc.Cues[i].Start + 4
		}
		if doc.Cues[i].End <= doc.Cues[i].Start {
			doc.Cues[i].End = doc.Cues[i].Start + 2
		}
	}
	mergeTrans(doc)
}

// mergeTrans 把时间几乎相同的相邻行合并成「原文 + 译文」。
// 双语 LRC 常见两种写法：同一时间戳两行，或中文行下面紧跟英文行。
func mergeTrans(doc *Doc) {
	if len(doc.Cues) == 0 {
		return
	}
	out := make([]Cue, 0, len(doc.Cues))
	for _, c := range doc.Cues {
		if len(out) > 0 {
			last := &out[len(out)-1]
			sameTime := abs(c.Start-last.Start) < 0.25
			if sameTime && last.Trans == "" && c.Text != last.Text {
				last.Trans = c.Text
				last.End = maxf(last.End, c.End)
				continue
			}
		}
		out = append(out, c)
	}
	doc.Cues = out
}

func shiftCues(cues []Cue, delta float64) {
	for i := range cues {
		cues[i].Start += delta
		if cues[i].End > 0 {
			cues[i].End += delta
		}
		for j := range cues[i].Words {
			cues[i].Words[j].T += delta
		}
	}
}

// MergeTrans 用另一份同曲歌词（通常是译文）补 Trans 字段。
func MergeTrans(base, extra *Doc) {
	if base == nil || extra == nil {
		return
	}
	for _, e := range extra.Cues {
		best := -1
		bestDelta := 1.5
		for i := range base.Cues {
			// 同一语言的歌词不需要互相补译文
			if hasCJK(base.Cues[i].Text) == hasCJK(e.Text) {
				continue
			}
			d := abs(base.Cues[i].Start - e.Start)
			if d < bestDelta {
				best, bestDelta = i, d
			}
		}
		if best >= 0 && base.Cues[best].Trans == "" && base.Cues[best].Text != e.Text {
			base.Cues[best].Trans = e.Text
		}
	}
}

// ToLRC 把 doc 写回 LRC 文本，用于「保存到媒体目录旁边」。
func ToLRC(doc *Doc) string {
	var sb strings.Builder
	if doc.Title != "" {
		fmt.Fprintf(&sb, "[ti:%s]\n", doc.Title)
	}
	if doc.Artist != "" {
		fmt.Fprintf(&sb, "[ar:%s]\n", doc.Artist)
	}
	if doc.Album != "" {
		fmt.Fprintf(&sb, "[al:%s]\n", doc.Album)
	}
	sb.WriteString("[by:mplay]\n")
	for _, c := range doc.Cues {
		min := int(c.Start) / 60
		sec := c.Start - float64(min*60)
		fmt.Fprintf(&sb, "[%02d:%05.2f]%s\n", min, sec, c.Text)
		if c.Trans != "" {
			fmt.Fprintf(&sb, "[%02d:%05.2f]%s\n", min, sec, c.Trans)
		}
	}
	return sb.String()
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
