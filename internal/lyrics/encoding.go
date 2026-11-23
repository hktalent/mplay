package lyrics

import (
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DecodeText 识别歌词/字幕文件的编码。
// 先严格校验 UTF-8（含 BOM / UTF-16），失败再按 GB18030 解 —— 中文老资源站
// 的 .lrc 绝大多数是 GBK 系编码，这一步不做的话整页都是乱码。
func DecodeText(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:])
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return utf16String(b[2:], false)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return utf16String(b[2:], true)
	}
	if isValidUTF8(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

func isValidUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			i++
		case c&0xE0 == 0xC0:
			if i+1 >= len(b) || b[i+1]&0xC0 != 0x80 {
				return false
			}
			i += 2
		case c&0xF0 == 0xE0:
			if i+2 >= len(b) || b[i+1]&0xC0 != 0x80 || b[i+2]&0xC0 != 0x80 {
				return false
			}
			i += 3
		case c&0xF8 == 0xF0:
			if i+3 >= len(b) || b[i+1]&0xC0 != 0x80 || b[i+2]&0xC0 != 0x80 || b[i+3]&0xC0 != 0x80 {
				return false
			}
			i += 4
		default:
			return false
		}
	}
	return true
}

func utf16String(b []byte, bigEndian bool) string {
	var sb strings.Builder
	sb.Grow(len(b) / 2)
	for i := 0; i+1 < len(b); i += 2 {
		var r uint16
		if bigEndian {
			r = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			r = uint16(b[i+1])<<8 | uint16(b[i])
		}
		if r == 0 {
			continue
		}
		if r >= 0xD800 && r <= 0xDBFF && i+3 < len(b) {
			var r2 uint16
			if bigEndian {
				r2 = uint16(b[i+2])<<8 | uint16(b[i+3])
			} else {
				r2 = uint16(b[i+3])<<8 | uint16(b[i+2])
			}
			if r2 >= 0xDC00 && r2 <= 0xDFFF {
				sb.WriteRune(rune(0x10000 + (uint32(r-0xD800) << 10) + uint32(r2-0xDC00)))
				i += 2
				continue
			}
		}
		sb.WriteRune(rune(r))
	}
	return sb.String()
}
