package media

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	// 资源站常见的垃圾后缀：码率、音质、来源、标签
	junkPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\s*[\[\(【（]\s*(320k|128k|flac|ape|wav|hires|hi-res|mp3|m4a|aac|ogg|无损|高清|标清|超清|完整版|官方版|正式版|原版|伴奏|纯音乐|dj版|remix)\s*[\]\)】）]`),
		regexp.MustCompile(`(?i)\s*[\[\(【（]\s*(mv|official\s*video|official|audio|lyric\s*video|lyrics|live|hq|hd|4k|1080p|720p)\s*[\]\)】）]`),
		regexp.MustCompile(`(?i)\s*-\s*(mp3|flac|ape|wav|m4a)$`),
	}
	trackNoPattern = regexp.MustCompile(`^\s*(?:[\(\[]?\d{1,3}[\)\]]?\s*[\.、\-_)]\s+|\d{1,3}\s+)`)
	featPattern    = regexp.MustCompile(`(?i)\s*(?:feat\.?|ft\.?|with)\s+`)
	spacesPattern  = regexp.MustCompile(`\s{2,}`)
)

// ParseFilename 在没有可靠标签时从文件名猜标题/艺人。
// 完整的多候选拆法在 GuessNames 里，这里只取排序第一的作为展示用结果；
// 真正的取舍交给歌词库校验（见 lyrics.BestMatch）。
func ParseFilename(name string) (title, artist string) {
	if gs := GuessNames(name); len(gs) > 0 {
		return gs[0].Title, gs[0].Artist
	}
	return strings.TrimSpace(filepath.Base(name)), ""
}

func isArtistLike(s string) bool {
	if s == "" || len([]rune(s)) > 18 {
		return false
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// NormalizeKey 用于歌词匹配：去空白/标点、全角转半角、小写。
func NormalizeKey(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r = r - 0xFF01 + 0x21
		case r == 0x3000:
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
