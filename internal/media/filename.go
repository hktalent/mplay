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
//
// 约定：常见命名是 "艺人 - 歌名"（含 01. 前缀、各种 [320k] 噪音）。
// 当左右两段长度悬殊或左边明显是歌名（带书名号/20 字以上）时，按 "歌名 - 艺人" 理解。
func ParseFilename(name string) (title, artist string) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = strings.ReplaceAll(base, "_", " ")
	for _, re := range junkPatterns {
		base = re.ReplaceAllString(base, " ")
	}
	base = trackNoPattern.ReplaceAllString(base, "")
	base = spacesPattern.ReplaceAllString(base, " ")
	base = strings.TrimSpace(base)
	if base == "" {
		return name, ""
	}

	// 《歌名》 or 《歌名》-艺人
	if i := strings.Index(base, "《"); i >= 0 {
		if j := strings.Index(base[i:], "》"); j > 0 {
			return strings.TrimSpace(base[i+len("《") : i+j]), ""
		}
	}

	parts := strings.Split(base, " - ")
	if len(parts) >= 2 {
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(strings.Join(parts[1:], " - "))
		if left == "" {
			return right, ""
		}
		if right == "" {
			return left, ""
		}
		// 左边更像歌名：带书名号、明显更长、或者在 feat 里
		if strings.Contains(left, "《") || len([]rune(left)) > 24 || featPattern.MatchString(left) {
			return left, strings.TrimSpace(featPattern.Split(right, 2)[0])
		}
		return right, left
	}

	// "歌名 by 艺人" / "歌名(艺人)"
	if i := strings.LastIndex(strings.ToLower(base), " by "); i > 0 {
		return strings.TrimSpace(base[:i]), strings.TrimSpace(base[i+4:])
	}
	if strings.HasSuffix(base, ")") {
		if l := strings.LastIndex(base, "("); l > 0 {
			inner := strings.TrimSpace(base[l+1 : len(base)-1])
			if isArtistLike(inner) {
				return strings.TrimSpace(base[:l]), inner
			}
		}
	}
	return base, ""
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
