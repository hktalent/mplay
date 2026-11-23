// Package desktop 把 urlbox（Rust + wry 的极简桌面外壳）接进来。
//
// 桌面端的形态刻意保持「一个本地服务 + 一个原生窗口」：
//   - mplay 本体负责扫目录、拉歌词、流式播放（跨平台同一份 Go 代码）；
//   - urlbox 只负责开一个系统 WebView 窗口，指向 127.0.0.1 上的服务。
//
// 这样同一套前端既能部署成网站，也能被 urlbox 包成桌面 App，不需要两套代码。
package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindShell 寻找 urlbox 可执行文件。顺序：环境变量 → PATH → 常见构建目录。
func FindShell() string {
	if p := os.Getenv("MPLAY_URLBOX"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("urlbox"); err == nil {
		return p
	}
	names := []string{"urlbox"}
	if runtime.GOOS == "windows" {
		names = []string{"urlbox.exe", "urlbox"}
	}
	exe, _ := os.Executable()
	dirs := []string{filepath.Dir(exe)}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, "MyWork", "urlbox", "target", "release"),
			filepath.Join(home, "MyWork", "urlbox", "dist"),
		)
	}
	dirs = append(dirs, "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin")
	for _, d := range dirs {
		for _, n := range names {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// Launch 用 urlbox 打开目标 URL，返回可 Wait 的进程。
func Launch(url string) (*exec.Cmd, error) {
	shell := FindShell()
	if shell == "" {
		return nil, fmt.Errorf("未找到 urlbox")
	}
	cmd := exec.Command(shell, url)
	cmd.Env = append(os.Environ(), "MPLAY_DESKTOP=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// OpenBrowser 是 urlbox 缺失时的兜底：用系统默认浏览器打开。
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// Reveal 在文件管理器里定位某个文件（桌面版「在文件夹中显示」）。
func Reveal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// OpenWithSystemPlayer 是格式不被 WebView 支持时的兜底：交给系统默认播放器。
func OpenWithSystemPlayer(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// IsLoopback 判断监听地址是否只对本机开放（决定是否允许「打开本地文件」这类动作）。
func IsLoopback(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "", "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// LoopbackURL 生成给 WebView/浏览器用的地址（0.0.0.0 监听时访问本机回环）。
func LoopbackURL(addr string) string {
	host, port := addr, "80"
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host, port = addr[:i], addr[i+1:]
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port + "/"
}
