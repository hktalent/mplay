package media

import (
	"os"
	"path/filepath"
	"testing"
)

// 回归：mp3 帧头 verBits==1 是标准里的保留值，srTable 里没有这一项。
// 旧代码先用它查表拿到 nil 切片，再 `sr[2]` 取值 → index out of range，
// 一个 12 字节的畸形 .mp3 就能让「重新扫描」把整个进程 panic 掉。
func TestMP3HeaderReservedVersionBits(t *testing.T) {
	// 0xFF 0xEA: 同步位 OK，verBits=1（保留值）、layer=1；0x90: brIdx=9, srIdx=0
	h := []byte{0xFF, 0xEA, 0x90, 0x00}
	br, sr, samples, side := mp3Header(h)
	if br != 0 || sr != 0 || samples != 0 || side != 0 {
		t.Fatalf("保留版本位应返回零值，实际 br=%d sr=%d samples=%d side=%d", br, sr, samples, side)
	}
	if d := mp3DurationFromBytes(h); d != 0 {
		t.Fatalf("畸形帧头不应算出时长，实际 %v", d)
	}
}

// mp3DurationFromBytes 把一段字节当成完整文件走一遍 mp3Duration 的同步搜索逻辑。
func mp3DurationFromBytes(b []byte) float64 {
	dir := os.TempDir()
	f, err := os.CreateTemp(dir, "mplay-mp3-*.bin")
	if err != nil {
		return 0
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return 0
	}
	return mp3Duration(f, 0, int64(len(b)))
}

// 回归：ReadTags 面对任意畸形文件都只能返回零值，不能 panic。
func TestReadTagsNeverPanics(t *testing.T) {
	cases := []struct {
		ext  string
		body []byte
	}{
		{".mp3", []byte{0xFF, 0xEA, 0x90, 0x00}},
		{".mp3", []byte{0xFF, 0xFB, 0x90, 0x64, 'X', 'i', 'n', 'g'}},
		{".mp3", []byte("ID3\x03\x00\x00\x00\x00\x00\x00")},
		{".aac", []byte{0xFF, 0xF1, 0x00}},
		{".flac", []byte("fLaC")},
		{".flac", append([]byte("fLaC\x00\x00\xff\xff"), 1, 2, 3)},
		{".m4a", []byte("\x00\x00\x00\x18ftypisom")},
		{".mp4", []byte("\x00\x00\x00\x00moov")},
		{".mov", []byte{0, 0, 0, 8, 'm', 'o', 'o', 'v'}},
		{".wav", []byte("RIFF????WAVE")},
	}
	for i, c := range cases {
		dir := t.TempDir()
		p := filepath.Join(dir, "x"+c.ext)
		if err := os.WriteFile(p, c.body, 0o644); err != nil {
			t.Fatalf("写测试文件失败: %v", err)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("第 %d 个畸形文件让 ReadTags panic: %v", i, r)
				}
			}()
			_ = ReadTags(p, c.ext)
		}()
	}
}

// 回归：曲库里混入畸形文件时，扫描要照常完成（其余条目正常入索引）。
func TestScanSurvivesMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	valid := makeAudioDir(t, 3)
	bad := filepath.Join(dir, "bad.mp3")
	if err := os.WriteFile(bad, []byte{0xFF, 0xEA, 0x90, 0x00}, 0o644); err != nil {
		t.Fatalf("写畸形文件失败: %v", err)
	}
	lib := NewLibrary(t.TempDir(), func() []string { return []string{valid, dir} })

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("扫描畸形文件时 panic: %v", r)
			}
		}()
		if err := lib.Scan(nil); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
	}()

	if got := lib.Stats()["audio"]; got != 4 { // 3 个正常 + 1 个按文件名兜底的畸形文件
		t.Fatalf("audio=%v 期望 4", got)
	}
}
