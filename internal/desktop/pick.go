package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrPickCanceled 表示用户在选择框里点了取消（不是错误，别再提示“失败”）。
var ErrPickCanceled = errors.New("已取消")

var ErrPickUnavailable = errors.New("当前环境没有可用的系统目录选择框")

// CanPickFolder 判断当前机器上有没有能力弹出系统目录选择框：
// 需要图形会话（Linux 看 DISPLAY/WAYLAND_DISPLAY），并且系统里有对应的原生工具。
// 纯 Web 服务器（systemd / 容器 / SSH）通常为 false，前端会自动退到浏览器的目录选择。
func CanPickFolder() bool {
	switch runtime.GOOS {
	case "darwin":
		return hasBin("osascript")
	case "windows":
		return hasBin("powershell") || hasBin("pwsh")
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		return hasBin("zenity") || hasBin("kdialog")
	}
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// PickFolder 弹出系统目录选择框并返回绝对路径；用户取消返回 ErrPickCanceled。
// prompt 是选择框里的提示语，留空用默认。
func PickFolder(ctx context.Context, prompt string) (string, error) {
	if !CanPickFolder() {
		return "", ErrPickUnavailable
	}
	if prompt == "" {
		prompt = "选择要加入曲库的文件夹"
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	var (
		out []byte
		err error
	)
	switch runtime.GOOS {
	case "darwin":
		script := `POSIX path of (choose folder with prompt "` + strings.ReplaceAll(prompt, `"`, `'`) + `")`
		out, err = exec.CommandContext(cctx, "osascript", "-e", script).Output()
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms;` +
			`$d = New-Object System.Windows.Forms.FolderBrowserDialog;` +
			`$d.Description = '` + strings.ReplaceAll(prompt, "'", "''") + `';` +
			`if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.SelectedPath }`
		shell := "powershell"
		if !hasBin(shell) && hasBin("pwsh") {
			shell = "pwsh"
		}
		out, err = exec.CommandContext(cctx, shell, "-NoProfile", "-STA", "-Command", ps).Output()
		if err != nil && shell == "pwsh" {
			out, err = exec.CommandContext(cctx, shell, "-NoProfile", "-Command", ps).Output()
		}
	default:
		if hasBin("zenity") {
			out, err = exec.CommandContext(cctx, "zenity", "--file-selection", "--directory",
				"--title="+prompt).Output()
		} else {
			out, err = exec.CommandContext(cctx, "kdialog", "--getexistingdirectory",
				os.Getenv("HOME"), "--title", prompt).Output()
		}
	}
	if err != nil {
		if cctx.Err() != nil {
			return "", cctx.Err()
		}
		// 取消时原生工具的退出码非 0，而且没有任何输出；区分不出取消还是失败时，
		// 以“没有路径”当作取消处理，比弹一个“失败”更贴近用户预期。
		return "", ErrPickCanceled
	}
	path := strings.TrimSpace(string(out))
	path = strings.TrimRight(path, "\r\n")
	if path == "" {
		return "", ErrPickCanceled
	}
	// macOS 的 choose folder 会带尾斜杠，去掉后再做校验
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "", ErrPickCanceled
	}
	st, statErr := os.Stat(path)
	if statErr != nil || !st.IsDir() {
		return "", ErrPickCanceled
	}
	return path, nil
}
