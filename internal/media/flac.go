package media

import (
	"io"
	"os"
	"strings"
)

// readFLAC 解析 STREAMINFO（时长）与 VORBIS_COMMENT（标题/艺人/歌词）以及 PICTURE（封面）。
func readFLAC(f *os.File, size int64) Tags {
	var t Tags
	head := make([]byte, 4)
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, 4), head); err != nil || string(head) != "fLaC" {
		return t
	}
	t.Source = "flac"
	off := int64(4)
	for off+4 <= size {
		hdr := readAtFull(f, off, 4)
		if hdr == nil {
			break
		}
		last := hdr[0]&0x80 != 0
		typ := hdr[0] & 0x7F
		blockLen := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		bodyOff := off + 4
		if blockLen <= 0 || bodyOff+blockLen > size {
			break
		}
		switch typ {
		case 0: // STREAMINFO
			if blockLen >= 18 {
				b := readAtFull(f, bodyOff, blockLen)
				if len(b) >= 18 {
					sampleRate := int(b[10])<<12 | int(b[11])<<4 | int(b[12])>>4
					totalSamples := int64(b[13]&0x0F)<<32 | int64(be32(b[14:18]))
					if sampleRate > 0 && totalSamples > 0 {
						t.Duration = clampTime(float64(totalSamples) / float64(sampleRate))
					}
				}
			}
		case 4: // VORBIS_COMMENT
			b := readAtFull(f, bodyOff, blockLen)
			if len(b) >= 8 {
				i := 0
				vendorLen := int(le32(b[0:4]))
				i = 4 + vendorLen
				if i+4 <= len(b) {
					count := int(le32(b[i : i+4]))
					i += 4
					for n := 0; n < count && i+4 <= len(b); n++ {
						l := int(le32(b[i : i+4]))
						i += 4
						if l <= 0 || i+l > len(b) {
							break
						}
						kv := string(b[i : i+l])
						i += l
						eq := strings.IndexByte(kv, '=')
						if eq <= 0 {
							continue
						}
						key := strings.ToUpper(kv[:eq])
						val := kv[eq+1:]
						switch key {
						case "TITLE":
							t.Title = val
						case "ARTIST":
							t.Artist = val
						case "ALBUM":
							t.Album = val
						case "ALBUMARTIST":
							t.AlbumArtist = val
						case "DATE", "YEAR":
							t.Year = firstYear(val)
						case "LYRICS", "UNSYNCEDLYRICS", "UNSYNCED LYRICS":
							if t.Lyrics == "" {
								t.Lyrics = val
							}
						case "SYNCEDLYRICS", "SYNCED LYRICS":
							if strings.Contains(val, "[") && strings.Contains(val, ":") {
								if t.SyncedLyrics == "" {
									t.SyncedLyrics = val
								}
							} else if t.Lyrics == "" {
								t.Lyrics = val
							}
						}
					}
				}
			}
		case 6: // PICTURE
			b := readAtFull(f, bodyOff, blockLen)
			if len(b) > 32 {
				i := 4
				mimeLen := int(be32(b[i : i+4]))
				i += 4
				// mime 是 ASCII，可能长于 len
				if mimeLen <= 0 || i+mimeLen > len(b) {
					break
				}
				mime := string(b[i : i+mimeLen])
				i += mimeLen
				descLen := int(be32(b[i : i+4]))
				i += 4 + descLen
				i += 16 // 宽高深色数
				if i+4 > len(b) {
					break
				}
				dataLen := int(be32(b[i : i+4]))
				i += 4
				if dataLen > 0 && i+dataLen <= len(b) {
					data := b[i : i+dataLen]
					m := sniffImage(data)
					if m == "" {
						m = mime
					}
					if strings.HasPrefix(m, "image/") {
						t.Cover = data
						t.CoverMIME = m
					}
				}
			}
		}
		off = bodyOff + blockLen
		if last {
			break
		}
	}
	return t
}
