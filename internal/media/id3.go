package media

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// readID3 读 ID3v2 标签并从第一个音频帧推算时长。
// 不引第三方 tag 库：这里只关心标题/艺人/专辑/歌词/封面/时长，自己写更可控，
// 也让交叉编译保持零 CGO、零外部依赖（除 x/text 的编码表）。
func readID3(f *os.File, size int64) Tags {
	var t Tags
	head := make([]byte, 10)
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, 10), head); err != nil || string(head[0:3]) != "ID3" {
		t.Duration = mp3Duration(f, 0, size)
		return t
	}

	ver := head[3]
	flags := head[5]
	tagSize := syncsafe(head[6:10])
	if tagSize <= 0 || int64(tagSize) > size {
		t.Duration = mp3Duration(f, 0, size)
		return t
	}
	// 16MB 上限：足以容纳任何正常封面，也防畸形文件把内存吃满。
	const maxTag = 16 << 20
	if tagSize > maxTag {
		tagSize = maxTag
	}

	body := make([]byte, tagSize)
	if _, err := io.ReadFull(io.NewSectionReader(f, 10, int64(tagSize)), body); err != nil && err != io.ErrUnexpectedEOF {
		t.Duration = mp3Duration(f, int64(10+tagSize), size)
		return t
	}
	if flags&0x80 != 0 { // unsynchronisation：按规范还原 FF 00 -> FF
		body = deunsync(body)
	}

	version := int(ver)
	frameHeader := 10
	idLen := 4
	if version == 2 {
		frameHeader = 6
		idLen = 3
	}

	for off := 0; off+frameHeader <= len(body); {
		id := string(body[off : off+idLen])
		if id == "" || id[0] == 0 {
			break
		}
		var fsize int
		if version == 2 {
			fsize = int(body[off+3])<<16 | int(body[off+4])<<8 | int(body[off+5])
		} else if version == 4 {
			fsize = syncsafe(body[off+4 : off+8])
		} else {
			fsize = int(be32(body[off+4 : off+8]))
		}
		if fsize <= 0 || off+frameHeader+fsize > len(body) {
			break
		}
		payload := body[off+frameHeader : off+frameHeader+fsize]
		t.Source = "id3"

		switch id {
		case "TIT2", "TT2":
			t.Title = id3Text(payload[0], payload[1:])
		case "TPE1", "TP1":
			t.Artist = id3Text(payload[0], payload[1:])
		case "TALB", "TAL":
			t.Album = id3Text(payload[0], payload[1:])
		case "TPE2", "TP2":
			t.AlbumArtist = id3Text(payload[0], payload[1:])
		case "TDRC", "TYER", "TYE":
			t.Year = firstYear(id3Text(payload[0], payload[1:]))
		case "USLT", "ULT":
			if len(payload) > 4 {
				// enc(1) + lang(3) + desc\0 + text
				i := 4
				for i < len(payload) && payload[i] != 0 {
					i++
				}
				if i+1 < len(payload) {
					t.Lyrics = strings.TrimSpace(id3Text(payload[0], payload[i+1:]))
				}
			}
		case "SYLT", "SLT":
			if lrc := parseSYLT(payload); lrc != "" {
				t.SyncedLyrics = lrc
			}
		case "APIC", "PIC":
			if data, mime := parseAPIC(payload, version == 2); len(data) > 0 {
				t.Cover = data
				t.CoverMIME = mime
			}
		}
		off += frameHeader + fsize
	}

	audioStart := int64(10 + tagSize)
	// 部分文件带 footer（flag 0x10），长度 10 字节。
	if flags&0x10 != 0 {
		audioStart += 10
	}
	t.Duration = mp3Duration(f, audioStart, size)
	return t
}

func firstYear(s string) string {
	if len(s) >= 4 && s[0] >= '0' && s[0] <= '9' {
		return s[:4]
	}
	return s
}

func deunsync(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xFF && i+1 < len(b) && b[i+1] == 0x00 {
			i++
		}
	}
	return out
}

func parseAPIC(payload []byte, v22 bool) ([]byte, string) {
	if len(payload) < 4 {
		return nil, ""
	}
	enc := payload[0]
	i := 1
	// MIME（v2.2 是 3 字符图片格式）
	mimeStart := i
	for i < len(payload) && payload[i] != 0 {
		i++
	}
	mime := string(payload[mimeStart:i])
	i++ // 跳过 0
	// 描述
	if enc == 1 || enc == 2 {
		for i+1 < len(payload) && !(payload[i] == 0 && payload[i+1] == 0) {
			i += 2
		}
		i += 2
	} else {
		for i < len(payload) && payload[i] != 0 {
			i++
		}
		i++
	}
	if i >= len(payload) {
		return nil, ""
	}
	data := payload[i:]
	if mime == "" || guessMime(v22, mime) == "" {
		mime = sniffImage(data)
	}
	if !strings.HasPrefix(mime, "image/") {
		mime = sniffImage(data)
	}
	if mime == "" {
		return nil, ""
	}
	return data, mime
}

func guessMime(v22 bool, m string) string {
	if v22 {
		switch strings.ToUpper(m) {
		case "PNG":
			return "image/png"
		case "JPG", "JPEG":
			return "image/jpeg"
		}
	}
	if strings.Contains(m, "/") {
		return m
	}
	return ""
}

// parseSYLT 把 ID3 同步歌词转成 LRC 文本，这样前端只需要一套解析器。
// payload: enc(1) lang(3) timestampFormat(1: 1=ms 2=frame) contentType(1) desc\0 content...
func parseSYLT(payload []byte) string {
	if len(payload) < 6 {
		return ""
	}
	enc := payload[0]
	format := payload[4]
	i := 6
	if enc == 1 || enc == 2 {
		for i+1 < len(payload) && !(payload[i] == 0 && payload[i+1] == 0) {
			i += 2
		}
		i += 2
	} else {
		for i < len(payload) && payload[i] != 0 {
			i++
		}
		i++
	}
	if format != 1 { // 只处理毫秒格式，帧格式罕见且需要采样率
		return ""
	}
	var sb strings.Builder
	for i < len(payload) {
		text, next := readID3String(payload, i, enc)
		if next <= i {
			break
		}
		i = next
		if i+4 > len(payload) {
			break
		}
		ms := int(be32(payload[i : i+4]))
		i += 4
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%02d:%05.2f]%s\n", ms/60000, float64(ms%60000)/1000, text))
	}
	return sb.String()
}

// readID3String 从 off 开始读一个以 0 结尾的字符串（UTF-16 用双 0 结尾），
// 返回文本和下一个可读位置。
func readID3String(payload []byte, off int, enc byte) (string, int) {
	if off >= len(payload) {
		return "", off
	}
	if enc == 1 || enc == 2 {
		i := off
		for i+1 < len(payload) && !(payload[i] == 0 && payload[i+1] == 0) {
			i += 2
		}
		text := id3Text(enc, payload[off:i])
		if i+1 < len(payload) {
			i += 2
		} else {
			i = len(payload)
		}
		return text, i
	}
	i := off
	for i < len(payload) && payload[i] != 0 {
		i++
	}
	text := id3Text(enc, payload[off:i])
	if i < len(payload) {
		i++
	}
	return text, i
}

// mp3Duration 先从 Xing/Info 头取总帧数，失败再按码率估算。
func mp3Duration(f *os.File, start, fileSize int64) float64 {
	if start < 0 {
		start = 0
	}
	probe := 8192
	if start+int64(probe) > fileSize {
		probe = int(fileSize - start)
	}
	if probe < 4 {
		return 0
	}
	buf := make([]byte, probe)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]

	syncAt := -1
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] == 0xFF && buf[i+1]&0xE0 == 0xE0 {
			if br, _, _, _ := mp3Header(buf[i : i+4]); br > 0 {
				syncAt = i
				break
			}
		}
	}
	if syncAt < 0 {
		return 0
	}
	h := buf[syncAt : syncAt+4]
	bitrate, sampleRate, samples, sideInfo := mp3Header(h)
	if sampleRate == 0 {
		return 0
	}
	frameStart := start + int64(syncAt)
	// 跳过 4 字节帧头 + side info
	xing := make([]byte, 16)
	if _, err := f.ReadAt(xing, frameStart+4+int64(sideInfo)); err == nil {
		if string(xing[0:4]) == "Xing" || string(xing[0:4]) == "Info" {
			flags := be32(xing[4:8])
			if flags&1 != 0 {
				frames := be32(xing[8:12])
				if frames > 0 {
					return float64(frames) * float64(samples) / float64(sampleRate)
				}
			}
		}
	}
	audioBytes := float64(fileSize - frameStart)
	if bitrate <= 0 {
		return 0
	}
	return audioBytes * 8 / float64(bitrate)
}

// mp3Header 返回 (bitrate bps, sampleRate, samplesPerFrame, sideInfoBytes)。
func mp3Header(b []byte) (int, int, int, int) {
	if len(b) < 4 {
		return 0, 0, 0, 0
	}
	verBits := (b[1] >> 3) & 0x03 // 3=MPEG1, 2=MPEG2, 0=MPEG2.5
	layer := (b[1] >> 1) & 0x03   // 1=Layer3
	brIdx := int((b[2] >> 4) & 0x0F)
	srIdx := int((b[2] >> 2) & 0x03)
	channel := (b[3] >> 6) & 0x03 // 3=单声道

	// verBits==1 是标准里的保留值（不是 MPEG1/2/2.5），也没有采样率表项：
	// 以前直接用它去查 srTable 会拿到 nil 切片，紧接着 sr[2] 就 index out of range
	// 把整个进程 panic 掉（一个畸形 mp3 文件就能触发）。
	if brIdx == 0 || brIdx == 15 || srIdx == 3 || layer != 1 || verBits == 1 {
		return 0, 0, 0, 0
	}

	mpeg1 := verBits == 3
	brTable := []int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	if !mpeg1 {
		brTable = []int{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	}
	srTable := map[int][]int{
		3: {44100, 48000, 32000},
		2: {22050, 24000, 16000},
		0: {11025, 12000, 8000},
	}
	br := brTable[brIdx-1] * 1000
	srs, ok := srTable[int(verBits)]
	if !ok || srIdx >= len(srs) {
		return 0, 0, 0, 0
	}
	sr := srs[srIdx]
	samples := 1152
	side := 32
	if mpeg1 {
		if channel == 3 {
			side = 17
		}
	} else {
		samples = 576
		side = 17
		if channel == 3 {
			side = 9
		}
	}
	return br, sr, samples, side
}

// FLAC / MP4 的少量工具
func clampTime(d float64) float64 {
	if d < 0 || d > 86400 || d != d {
		return 0
	}
	return d
}

func readAtFull(r io.ReaderAt, off, n int64) []byte {
	b := make([]byte, n)
	if _, err := r.ReadAt(b, off); err != nil {
		return nil
	}
	return b
}

func timeSince(t time.Time) string { return time.Since(t).Round(time.Second).String() }
