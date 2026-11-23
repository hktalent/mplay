package media

import (
	"io"
	"os"
	"strings"
)

type mp4Box struct {
	typ   string
	start int64 // 整个 box 的起点
	end   int64 // 整个 box 的终点（不含）
	body  int64 // 内容起点
}

func readMP4Box(r io.ReaderAt, off, end int64) (mp4Box, bool) {
	if off+8 > end {
		return mp4Box{}, false
	}
	hdr := make([]byte, 16)
	if _, err := r.ReadAt(hdr[:8], off); err != nil {
		return mp4Box{}, false
	}
	size := int64(be32(hdr[0:4]))
	typ := string(hdr[4:8])
	headerLen := int64(8)
	if size == 1 {
		if _, err := r.ReadAt(hdr[8:16], off+8); err != nil {
			return mp4Box{}, false
		}
		size = int64(uint64(be32(hdr[8:12]))<<32 | uint64(be32(hdr[12:16])))
		headerLen = 16
	} else if size == 0 {
		size = end - off
	}
	if size < headerLen || off+size > end {
		return mp4Box{}, false
	}
	return mp4Box{typ: typ, start: off, end: off + size, body: off + headerLen}, true
}

func mp4Children(r io.ReaderAt, start, end int64, skip int64) []mp4Box {
	var out []mp4Box
	for off := start + skip; off+8 <= end; {
		b, ok := readMP4Box(r, off, end)
		if !ok {
			break
		}
		out = append(out, b)
		off = b.end
	}
	return out
}

// findBoxPath 沿路径下钻，例如 moov → udta → meta → ilst。
// meta 是 full box，内容前面有 4 字节 version/flags，需要额外跳过。
func findBoxPath(r io.ReaderAt, start, end int64, path ...string) (mp4Box, bool) {
	curStart, curEnd := start, end
	var cur mp4Box
	found := false
	for _, want := range path {
		children := mp4Children(r, curStart, curEnd, 0)
		hit := false
		for _, c := range children {
			if c.typ == want {
				cur = c
				curStart, curEnd = c.body, c.end
				if want == "meta" {
					curStart += 4
				}
				hit = true
				break
			}
		}
		if !hit {
			return mp4Box{}, false
		}
		found = true
	}
	return cur, found
}

// readMP4 取 mvhd 时长与 ilst 标签（m4a / mp4）。
func readMP4(f *os.File, size int64) Tags {
	var t Tags
	t.Source = "mp4"
	if size < 16 {
		return t
	}

	if moov, ok := findBoxPath(f, 0, size, "moov"); ok {
		for _, c := range mp4Children(f, moov.body, moov.end, 0) {
			if c.typ != "mvhd" {
				continue
			}
			b := readAtFull(f, c.body, c.end-c.body)
			if len(b) >= 20 && b[0] == 0 {
				timescale := be32(b[12:16])
				duration := be32(b[16:20])
				if timescale > 0 {
					t.Duration = clampTime(float64(duration) / float64(timescale))
				}
			} else if len(b) >= 32 && b[0] == 1 {
				timescale := be32(b[20:24])
				dur := uint64(be32(b[24:28]))<<32 | uint64(be32(b[28:32]))
				if timescale > 0 {
					t.Duration = clampTime(float64(dur) / float64(timescale))
				}
			}
		}
	}

	ilst, ok := findBoxPath(f, 0, size, "moov", "udta", "meta", "ilst")
	if !ok {
		return t
	}
	for _, item := range mp4Children(f, ilst.body, ilst.end, 0) {
		if len(item.typ) < 4 {
			continue
		}
		key := item.typ
		if key[0] == 0xA9 { // © 符号在 mp4 里就是 0xA9
			key = "©" + key[1:]
		}
		data, ok := findBoxPath(f, item.body, item.end, "data")
		if !ok {
			continue
		}
		payload := readAtFull(f, data.body+8, data.end-(data.body+8))
		if payload == nil {
			continue
		}
		switch key {
		case "©nam":
			t.Title = strings.TrimSpace(string(payload))
		case "©ART":
			t.Artist = strings.TrimSpace(string(payload))
		case "©alb":
			t.Album = strings.TrimSpace(string(payload))
		case "aART":
			t.AlbumArtist = strings.TrimSpace(string(payload))
		case "©day":
			t.Year = firstYear(strings.TrimSpace(string(payload)))
		case "©lyr":
			t.Lyrics = strings.TrimSpace(string(payload))
		case "covr":
			mime := sniffImage(payload)
			if strings.HasPrefix(mime, "image/") {
				t.Cover = payload
				t.CoverMIME = mime
			}
		}
	}
	return t
}
