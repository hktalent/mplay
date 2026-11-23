package lyrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SubtitleClient 是可选的 OpenSubtitles 客户端：本地没有同名字幕时，
// 用「清理过的文件名」去公开字幕库检索。需要自己申请免费 API Key，
// 因此在默认配置里是关闭的（未配置 key 时 Enabled()==false）。
type SubtitleClient struct {
	APIKey   string
	Username string
	Password string

	CacheDir string
	HTTP     *http.Client

	mu    sync.Mutex
	token string
}

// NewSubtitleClient 创建字幕客户端。
func NewSubtitleClient(apiKey, username, password, cacheDir string) *SubtitleClient {
	return &SubtitleClient{
		APIKey:   apiKey,
		Username: username,
		Password: password,
		CacheDir: cacheDir,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        10,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// Enabled 是否配置了 API Key。
func (c *SubtitleClient) Enabled() bool { return c != nil && strings.TrimSpace(c.APIKey) != "" }

func (c *SubtitleClient) headers() map[string]string {
	h := map[string]string{
		"Api-Key":      c.APIKey,
		"User-Agent":   "mplay v1.0",
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	if t := c.getToken(); t != "" {
		h["Authorization"] = "Bearer " + t
	}
	return h
}

func (c *SubtitleClient) getToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *SubtitleClient) login(ctx context.Context) {
	if c.Username == "" || c.Password == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"username": c.Username, "password": c.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.opensubtitles.com/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) == nil && out.Token != "" {
		c.mu.Lock()
		c.token = out.Token
		c.mu.Unlock()
	}
}

// Fetch 用 query 检索并下载第一条字幕，返回 SRT 文本与文件名。
// languages 用 OpenSubtitles 的语言码，如 zh-cn、en。
func (c *SubtitleClient) Fetch(ctx context.Context, query string, languages []string) ([]byte, string, error) {
	if !c.Enabled() {
		return nil, "", errors.New("未配置 OpenSubtitles API Key")
	}
	if strings.TrimSpace(query) == "" {
		return nil, "", errors.New("字幕检索缺少关键词")
	}
	if len(languages) == 0 {
		languages = []string{"zh-cn", "zh-tw", "en"}
	}
	v := url.Values{}
	v.Set("query", query)
	v.Set("languages", strings.Join(languages, ","))
	v.Set("order_by", "download_count")
	v.Set("order_direction", "desc")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.opensubtitles.com/api/v1/subtitles?"+v.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	for k, val := range c.headers() {
		req.Header.Set(k, val)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("opensubtitles 检索失败: HTTP %d", resp.StatusCode)
	}

	var searchResp struct {
		Data []struct {
			Attributes struct {
				Files []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
				FeatureDetails struct {
					Title string `json:"title"`
					Year  string `json:"year"`
				} `json:"feature_details"`
				Language string `json:"language"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&searchResp); err != nil {
		return nil, "", err
	}
	if len(searchResp.Data) == 0 || len(searchResp.Data[0].Attributes.Files) == 0 {
		return nil, "", ErrNotFound
	}
	fileID := searchResp.Data[0].Attributes.Files[0].FileID
	fileName := searchResp.Data[0].Attributes.Files[0].FileName

	// 第一次 401 时尝试登录再重试一次
	link, err := c.downloadLink(ctx, fileID)
	if err != nil {
		c.login(ctx)
		link, err = c.downloadLink(ctx, fileID)
		if err != nil {
			return nil, "", err
		}
	}
	dreq, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	dreq.Header.Set("User-Agent", "mplay v1.0")
	dresp, err := c.HTTP.Do(dreq)
	if err != nil {
		return nil, "", err
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("下载字幕失败: HTTP %d", dresp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(dresp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	return data, fileName, nil
}

func (c *SubtitleClient) downloadLink(ctx context.Context, fileID int) (string, error) {
	body, _ := json.Marshal(map[string]int{"file_id": fileID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenSubtitles 下载接口 HTTP %d（可能需要登录或配额用尽）", resp.StatusCode)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Link == "" {
		return "", errors.New("OpenSubtitles 未返回下载地址")
	}
	return out.Link, nil
}

// CachePath 是某条媒体的字幕缓存路径。
func (c *SubtitleClient) CachePath(key string) string {
	return filepath.Join(c.CacheDir, "subtitles", key+".srt")
}

// ReadCache 读已下载的字幕。
func (c *SubtitleClient) ReadCache(key string) ([]byte, error) {
	return os.ReadFile(c.CachePath(key))
}

// WriteCache 保存字幕，避免重复消耗 API 配额。
func (c *SubtitleClient) WriteCache(key string, data []byte) error {
	dir := filepath.Dir(c.CachePath(key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.CachePath(key), data, 0o644)
}
