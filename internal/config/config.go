// Package config 负责 mplay 的配置读写：命令行 > 环境变量 > 配置文件 > 默认值。
//
// 配置只有一个 JSON 文件，好处是桌面版可以直接让用户编辑，web 版也能通过
// /api/config 读出来展示；不需要数据库，也不会因为升级而失效。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Source 是一个「自定义音源」的描述。它只约定一件事：给一个搜索地址的模板，
// 返回 JSON，然后告诉 mplay 从哪个数组、哪个字段取数据。
// 这样既能把任何自有接口接进来，又不会在代码里内置任何第三方抓取逻辑。
type Source struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Search  string            `json:"searchUrl"`           // 支持 {q} {limit}
	Items   string            `json:"itemsPath,omitempty"` // 点号路径，留空表示根就是数组
	Fields  map[string]string `json:"fields"`              // title/artist/album/duration/stream/cover/page
	Headers map[string]string `json:"headers,omitempty"`
	Note    string            `json:"note,omitempty"`
}

// Config 是全部可配置项。
type Config struct {
	Addr         string   `json:"addr"`
	MediaDirs    []string `json:"mediaDirs"`
	WriteSidecar bool     `json:"writeSidecar"`
	AllowLAN     bool     `json:"allowLan"`

	EnableITunes       bool `json:"enableITunes"`
	EnableArchive      bool `json:"enableArchive"`
	EnableLRCLIB       bool `json:"enableLRCLIB"`
	EnableNeteaseLyric bool `json:"enableNeteaseLyricSearch"`

	LRCLIBBase      string `json:"lrclibBase"`
	JamendoClientID string `json:"jamendoClientId"`

	OSAPIKey   string `json:"opensubtitlesApiKey"`
	OSUsername string `json:"opensubtitlesUsername,omitempty"`
	OSPassword string `json:"opensubtitlesPassword,omitempty"`

	Language string   `json:"language"`
	Sources  []Source `json:"sources"`

	// AuthToken 非空时，/api 与 /media 都要求带 token（公开部署时防止曲库裸奔）。
	AuthToken string `json:"authToken,omitempty"`

	path string
}

// Path 返回配置文件路径。
func (c *Config) Path() string { return c.path }

// Default 生成一份默认配置：媒体目录只取真实存在的几个常见目录。
func Default() *Config {
	cfg := &Config{
		Addr:               "127.0.0.1:8737",
		MediaDirs:          defaultMediaDirs(),
		EnableITunes:       true,
		EnableArchive:      true,
		EnableLRCLIB:       true,
		EnableNeteaseLyric: true,
		LRCLIBBase:         "https://lrclib.net",
		Language:           "zh-CN",
		Sources:            []Source{},
	}
	cfg.path = filepath.Join(ConfigDir(), "config.json")
	return cfg
}

// ConfigDir 是配置目录（~/.config/mplay 或平台等价目录）。
func ConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.Getenv("HOME")
	}
	return filepath.Join(base, "mplay")
}

// CacheDir 是缓存目录（曲库索引、歌词、封面）。
func CacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "mplay")
}

func defaultMediaDirs() []string {
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, "Music"),
		filepath.Join(home, "音乐"),
		filepath.Join(home, "Movies"),
		filepath.Join(home, "Downloads"),
	}
	out := make([]string, 0, len(cands))
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// Load 读取配置；文件不存在时创建一份默认配置。
func Load(path string) (*Config, error) {
	if path == "" {
		path = filepath.Join(ConfigDir(), "config.json")
	}
	cfg := Default()
	cfg.path = path

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
		}
		cfg.path = path
	case errors.Is(err, os.ErrNotExist):
		if err := cfg.Save(); err != nil {
			// 配置目录不可写不算致命，继续用内存里的默认值。
			fmt.Fprintf(os.Stderr, "[warn] 无法写入默认配置 %s: %v\n", path, err)
		}
	default:
		return nil, fmt.Errorf("读取配置 %s 失败: %w", path, err)
	}
	return cfg, nil
}

// Save 原子写入配置文件（先写临时文件再 rename），避免半截文件。
func (c *Config) Save() error {
	if c.path == "" {
		c.path = filepath.Join(ConfigDir(), "config.json")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	c.Normalize()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Normalize 清洗用户填的目录：展开 ~、转绝对路径、去重。
func (c *Config) Normalize() {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8737"
	}
	if c.Language == "" {
		c.Language = "zh-CN"
	}
	if c.LRCLIBBase == "" {
		c.LRCLIBBase = "https://lrclib.net"
	}
	c.LRCLIBBase = strings.TrimRight(c.LRCLIBBase, "/")

	seen := map[string]bool{}
	dirs := make([]string, 0, len(c.MediaDirs))
	for _, raw := range c.MediaDirs {
		for _, part := range strings.Split(raw, string(os.PathListSeparator)) {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			p = ExpandHome(p)
			if !filepath.IsAbs(p) {
				if abs, err := filepath.Abs(p); err == nil {
					p = abs
				}
			}
			p = filepath.Clean(p)
			if seen[p] {
				continue
			}
			seen[p] = true
			dirs = append(dirs, p)
		}
	}
	c.MediaDirs = dirs
	if c.Sources == nil {
		c.Sources = []Source{}
	}
}

// ExpandHome 把 ~ 展开成用户主目录。
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err == nil {
			if len(p) <= 2 {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// ApplyEnv 应用环境变量覆盖（容器/服务部署时最方便）。
func (c *Config) ApplyEnv() {
	if v := os.Getenv("MPLAY_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("MPLAY_MEDIA"); v != "" {
		c.MediaDirs = append(c.MediaDirs, strings.Split(v, string(os.PathListSeparator))...)
	}
	if v := os.Getenv("MPLAY_LAN"); v == "1" || strings.EqualFold(v, "true") {
		c.AllowLAN = true
	}
	if v := os.Getenv("MPLAY_JAMENDO_CLIENT_ID"); v != "" {
		c.JamendoClientID = v
	}
	if v := os.Getenv("MPLAY_OS_API_KEY"); v != "" {
		c.OSAPIKey = v
	}
	if v := os.Getenv("MPLAY_TOKEN"); v != "" {
		c.AuthToken = v
	}
	c.Normalize()
}

// DesktopHint 给 UI 用的说明：当前是否跑在桌面壳里。
// 判断依据不是操作系统，而是有没有被 urlbox 拉起（见 internal/desktop）。
func IsDesktop() bool {
	return os.Getenv("MPLAY_DESKTOP") == "1"
}

// Platform 返回简化平台名，用于 UI 文案。
func Platform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}
