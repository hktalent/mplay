package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeAudioDir 造 n 个极小的可解析音频文件，返回目录路径。
func makeAudioDir(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	hdr := []byte("ID3\x03\x00\x00\x00\x00\x00\x00" + string(make([]byte, 40)))
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("song%05d.mp3", i))
		if err := os.WriteFile(p, hdr, 0o644); err != nil {
			t.Fatalf("造测试文件失败: %v", err)
		}
	}
	return dir
}

func waitScanningDone(t *testing.T, l *Library, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// 还有排队中的扫描请求时 Scanning() 仍可能为 false，
		// 这里额外等一下让 worker 把队列消化掉。
		if !l.Scanning() {
			time.Sleep(200 * time.Millisecond)
			if !l.Scanning() {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%v 内扫描没有结束（可能卡住）", timeout)
}

// 回归：POST /api/config 改目录时曾经传 nil context，
// WalkDir 回调里 ctx.Err() 直接空指针 panic 把整个服务打崩。
func TestScanNilContextDoesNotPanic(t *testing.T) {
	dir := makeAudioDir(t, 3)
	lib := NewLibrary(t.TempDir(), func() []string { return []string{dir} })

	err := lib.Scan(nil) // 故意传 nil
	if err != nil {
		t.Fatalf("Scan(nil) 返回错误: %v", err)
	}
	if got := lib.Stats()["audio"]; got != 3 {
		t.Fatalf("扫描结果不对，audio=%v 期望 3", got)
	}
}

// 回归：连续多次移除目录时，若扫描正在进行，直接 Scan 会拿到「扫描正在进行中」
// 并把这次变更丢掉（曲库残留旧条目）。RequestScan 必须保证最后一次变更被扫到。
func TestRequestScanWhileBusyIsNotDropped(t *testing.T) {
	big := makeAudioDir(t, 6000)
	roots := []string{big}
	lib := NewLibrary(t.TempDir(), func() []string { return roots })

	// 用同步 Scan 占住扫描位（模拟用户点「重新扫描」后正在转圈）。
	busyErr := make(chan error, 1)
	go func() { busyErr <- lib.Scan(context.Background()) }()

	// 等真的进入扫描状态。
	deadline := time.Now().Add(5 * time.Second)
	for !lib.Scanning() {
		if time.Now().After(deadline) {
			t.Fatal("扫描没有开始")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 扫描进行中把目录清空，并请求扫描（等价于用户连续移除了最后一个目录）。
	roots = nil
	lib.RequestScan()

	if err := <-busyErr; err != nil {
		t.Fatalf("第一次扫描失败: %v", err)
	}

	// 补扫必须发生：最终曲库应清空。
	deadline = time.Now().Add(30 * time.Second)
	for {
		if !lib.Scanning() && lib.Stats()["audio"] == 0 && lib.Stats()["roots"] == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("目录清空后没有被补扫，仍是 audio=%v roots=%v",
				lib.Stats()["audio"], lib.Stats()["roots"])
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// 回归：连续改目录时多次扫描排队只应合并成一次（容量 1 的队列语义）。
func TestRequestScanCoalesces(t *testing.T) {
	dir := makeAudioDir(t, 2)
	lib := NewLibrary(t.TempDir(), func() []string { return []string{dir} })

	for i := 0; i < 20; i++ {
		lib.RequestScan()
	}
	waitScanningDone(t, lib, 30*time.Second)
	if got := lib.Stats()["audio"]; got != 2 {
		t.Fatalf("合并扫描后 audio=%v 期望 2", got)
	}
}
