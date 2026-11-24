// Package identify 负责回答一个问题：这首歌到底叫什么？
//
// 三种线索按成本从低到高使用：
//  1. 本地：内嵌标签 + 文件名解析（离线、零成本、默认可用）；
//  2. LRCLIB：拿本地候选去公共歌词库比对，用歌名/歌手相似度 + 时长校验；
//  3. AI：配置了 OpenAI 兼容接口时，把文本线索（可选加音频片段）交给模型判断，
//     再用 LRCLIB 校验 AI 的答案，避免模型编造不存在的专辑。
//
// 原则：宁可返回「没把握」，也不把猜测覆盖掉用户文件里已有的可信标签。
package identify

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
)

// Candidate 是一条「这首歌可能是」的猜测。
type Candidate struct {
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album,omitempty"`
	Confidence float64 `json:"confidence"` // 0..1
	Source     string  `json:"source"`     // tag / filename / lrclib / ai-text / ai-audio
	Verified   bool    `json:"verified"`   // 是否被歌词库证实过
	Note       string  `json:"note,omitempty"`
}

// Hints 是喂给识别流程的全部线索。
type Hints struct {
	Name         string // 文件名（含扩展名）
	Path         string // 绝对路径，音频模式下要读它
	Ext          string // .mp3 / .flac ...
	Title        string // 现有标签
	Artist       string
	Album        string
	Duration     float64
	LyricSnippet string // 已有歌词/字幕的前几行，常能直接吐出歌名
}

// Options 是 AI 部分的配置。Enabled=false 时整条 AI 链路不参与。
type Options struct {
	Enabled       bool
	BaseURL       string // OpenAI 兼容，如 https://api.openai.com/v1
	APIKey        string
	Model         string
	AllowAudio    bool  // 是否允许把音频片段发给服务
	MaxAudioBytes int64 // 单次上传上限，默认 6MB
	Timeout       time.Duration
	// AllowPrivate 只用于测试：放行指向 127.0.0.1 的端点（httptest）。
	// 正常运行时必须为 false，否则 AI 端点会变成一个访问内网的跳板。
	AllowPrivate bool
}

// Ready 报告 AI 这条链路是否可用。
func (o Options) Ready() bool {
	return o.Enabled && strings.TrimSpace(o.BaseURL) != "" && strings.TrimSpace(o.Model) != ""
}

// LocalCandidates 只用本地线索生成候选，按可信度从高到低。
func LocalCandidates(h Hints) []Candidate {
	out := make([]Candidate, 0, 4)
	add := func(c Candidate) {
		c.Title = strings.TrimSpace(c.Title)
		c.Artist = strings.TrimSpace(c.Artist)
		c.Album = strings.TrimSpace(c.Album)
		if c.Title == "" && c.Artist == "" {
			return
		}
		key := media.NormalizeKey(c.Title + "|" + c.Artist)
		for _, e := range out {
			if media.NormalizeKey(e.Title+"|"+e.Artist) == key {
				return
			}
		}
		out = append(out, c)
	}

	// 1. 标签：文件自己写的，最可信。缺一边时用文件名补另一边。
	if h.Title != "" || h.Artist != "" {
		c := Candidate{Title: h.Title, Artist: h.Artist, Album: h.Album, Confidence: 0.9, Source: "tag"}
		if c.Artist == "" {
			if _, a := media.ParseFilename(h.Name); a != "" {
				c.Artist = a
			}
		}
		if c.Title == "" {
			if t, _ := media.ParseFilename(h.Name); t != "" {
				c.Title = t
			}
		}
		add(c)
	}

	// 2. 文件名：常见「艺人 - 歌名」，也有反着写的，两种都留作候选。
	tFile, aFile := media.ParseFilename(h.Name)
	if tFile != "" || aFile != "" {
		add(Candidate{Title: tFile, Artist: aFile, Confidence: 0.55, Source: "filename"})
		if aFile != "" {
			add(Candidate{Title: aFile, Artist: tFile, Confidence: 0.4, Source: "filename", Note: "文件名顺序反过来试一次"})
		}
		// 去掉括号补充信息后再来一版：《歌名 (Live)》→《歌名》
		if t, _ := media.ParseFilename(stripParen(tFile)); t != "" && t != tFile {
			add(Candidate{Title: t, Artist: aFile, Confidence: 0.5, Source: "filename", Note: "去掉括号信息"})
		}
	}

	// 3. 歌词片段当作自由文本搜（不构造成候选，由 Verify 单独处理）
	return out
}

func stripParen(s string) string {
	for _, pair := range [][2]string{{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"}} {
		if i := strings.Index(s, pair[0]); i > 0 {
			if j := strings.Index(s[i:], pair[1]); j > 0 {
				s = strings.TrimSpace(s[:i] + s[i+j+len(pair[1]):])
			}
		}
	}
	return s
}

// Verify 用歌词库校验一条候选：搜到记录后按歌名/歌手相似度和时长打分。
// 返回校验后的候选（Album 用库里的值补齐）以及是否达到可信阈值。
func Verify(ctx context.Context, lc *lyrics.Client, cand Candidate, duration float64) (Candidate, bool) {
	if lc == nil || !lc.Enabled {
		return cand, false
	}
	recs, err := lc.SearchTrack(ctx, cand.Title, cand.Artist)
	if err != nil || len(recs) == 0 {
		// 带艺人的没搜到，退一步只按歌名搜
		if cand.Title != "" {
			recs, err = lc.SearchTrack(ctx, cand.Title, "")
		}
		if err != nil || len(recs) == 0 {
			return cand, false
		}
	}

	best := lyrics.Record{}
	bestScore := 0.0
	for _, r := range recs {
		sc := score(cand, r, duration)
		if sc > bestScore {
			best, bestScore = r, sc
		}
	}
	if bestScore < 0.62 {
		cand.Confidence = math.Max(cand.Confidence*0.5, 0.2)
		cand.Note = "歌词库里没有足够接近的记录"
		return cand, false
	}

	out := cand
	out.Verified = true
	out.Confidence = math.Min(0.99, math.Max(cand.Confidence, bestScore))
	out.Source = "lrclib"
	if best.ArtistName != "" {
		out.Artist = best.ArtistName
	}
	if best.TrackName != "" {
		out.Title = best.TrackName
	}
	if best.AlbumName != "" {
		out.Album = best.AlbumName
	}
	return out, true
}

func score(cand Candidate, r lyrics.Record, duration float64) float64 {
	t := sim(cand.Title, r.TrackName)
	a := 1.0
	if cand.Artist != "" && r.ArtistName != "" {
		a = sim(cand.Artist, r.ArtistName)
	}
	d := 1.0
	if duration > 30 && r.Duration > 30 {
		delta := math.Abs(duration - r.Duration)
		switch {
		case delta <= 3:
			d = 1.0
		case delta <= 10:
			d = 0.85
		case delta <= 30:
			d = 0.7
		default:
			d = 0.35
		}
	}
	// 歌名权重最高：艺人和时长只是辅助确认。
	return 0.62*t + 0.23*a + 0.15*d
}

func sim(a, b string) float64 {
	ka, kb := media.NormalizeKey(a), media.NormalizeKey(b)
	if ka == "" || kb == "" {
		return 0
	}
	if ka == kb {
		return 1
	}
	if strings.Contains(ka, kb) || strings.Contains(kb, ka) {
		return 0.86
	}
	// 字符二元组 Jaccard，够用且不引人依赖
	ga, gb := grams(ka), grams(kb)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	inter := 0
	for k := range ga {
		if _, ok := gb[k]; ok {
			inter++
		}
	}
	union := len(ga) + len(gb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func grams(s string) map[string]int {
	rs := []rune(s)
	m := make(map[string]int, len(rs))
	if len(rs) == 1 {
		m[string(rs)] = 1
		return m
	}
	for i := 0; i+1 < len(rs); i++ {
		m[string(rs[i:i+2])]++
	}
	return m
}
