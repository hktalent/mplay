package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Tags 是从容器里能读出来的元信息。读不出来不算错误——文件名解析会兜底。
type Tags struct {
	Title        string
	Artist       string
	Album        string
	AlbumArtist  string
	Year         string
	Lyrics       string // 纯文本歌词（USLT / FLAC LYRICS）
	SyncedLyrics string // 已经带时间戳的歌词（SYLT 转换而来，LRC 格式）
	Cover        []byte
	CoverMIME    string
	Duration     float64
	Source       string // id3 / flac / mp4 / ""
}

// ReadTags 按扩展名分派到对应的容器解析器。任何解析失败都返回零值，不报错。
func ReadTags(path, ext string) (tags Tags) {
	// 解析的是用户磁盘上的任意文件：任何越界/畸形输入都只能让这一个文件读不出标签，
	// 绝不能把扫描进程带崩（以前一个畸形 mp3 就能 panic 掉整个服务）。
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[mplay] 解析标签异常 %s: %v\n", path, r)
			tags = Tags{}
		}
	}()

	f, err := os.Open(path)
	if err != nil {
		return Tags{}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Tags{}
	}

	switch strings.ToLower(ext) {
	case ".mp3", ".aac", ".ape":
		return readID3(f, st.Size())
	case ".flac":
		return readFLAC(f, st.Size())
	case ".m4a", ".m4b", ".mp4", ".m4v", ".mov":
		return readMP4(f, st.Size())
	}
	return Tags{}
}

// decodeText 处理歌词/字幕文件编码。中文歌词绝大多数是 UTF-8，但老资源站
// 下来的 .lrc 常常是 GBK/GB18030 —— 先严格试 UTF-8，失败再按 GB18030 解。
func decodeText(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:])
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return utf16ToString(b[2:], false)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return utf16ToString(b[2:], true)
	}
	// UTF-8 严格校验：含非法字节时再走 GB18030。
	if validUTF8(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	// 最后兜底：按 Latin-1 逐个映射，至少不会丢结构。
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

func validUTF8(b []byte) bool {
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

func utf16ToString(b []byte, bigEndian bool) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		var r uint16
		if bigEndian {
			r = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			r = uint16(b[i+1])<<8 | uint16(b[i])
		}
		if r == 0 {
			// 保留剩余内容而不是直接截断，某些 ID3 帧里有多个字符串。
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
	return strings.TrimRight(sb.String(), "\x00")
}

// id3Text 解 ID3 文本帧：编码字节 + 内容。
func id3Text(enc byte, b []byte) string {
	switch enc {
	case 0:
		// 规范说 0 = ISO-8859-1，但中文 MP3 几乎都偷偷塞 GBK。
		// 先按 UTF-8 校验，失败再按 GB18030 解，最后才是 Latin-1。
		return decodeLegacy(b)
	case 1:
		if len(b) >= 2 {
			if b[0] == 0xFF && b[1] == 0xFE {
				return utf16ToString(b[2:], false)
			}
			if b[0] == 0xFE && b[1] == 0xFF {
				return utf16ToString(b[2:], true)
			}
		}
		return utf16ToString(b, false)
	case 2:
		return utf16ToString(b, true)
	default:
		text := strings.TrimRight(string(b), "\x00 ")
		if validUTF8([]byte(text)) {
			return text
		}
		return decodeLegacy(b)
	}
}

// decodeLegacy 处理「标称 Latin-1 实际是 GBK」的文本。
func decodeLegacy(b []byte) string {
	b = bytesTrimNul(b)
	if len(b) == 0 {
		return ""
	}
	if validUTF8(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil && validUTF8(out) {
		return strings.TrimSpace(string(out))
	}
	rs := make([]rune, 0, len(b))
	for _, c := range b {
		if c == 0 {
			continue
		}
		rs = append(rs, rune(c))
	}
	return strings.TrimSpace(string(rs))
}

func bytesTrimNul(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == 0 || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func syncsafe(b []byte) int {
	return int(b[0]&0x7F)<<21 | int(b[1]&0x7F)<<14 | int(b[2]&0x7F)<<7 | int(b[3]&0x7F)
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func le32(b []byte) uint32 {
	return uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
}

// sniffImage 按文件头判断封面类型，避免把 png 存成 jpg。
func sniffImage(b []byte) string {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8:
		return "image/jpeg"
	case len(b) > 8 && string(b[1:4]) == "PNG":
		return "image/png"
	case len(b) > 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func extForMIME(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func baseNoExt(name string) string {
	name = filepath.Base(name)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func debugf(format string, args ...any) {
	if os.Getenv("MPLAY_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[mplay] "+format+"\n", args...)
	}
}
