package identify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client 调用 OpenAI 兼容的 /chat/completions 接口做识别。
// 只依赖一个可配置的 HTTP 端点，因此豆包 Ark、OpenAI、自建兼容网关都能直接用。
type Client struct {
	Opt  Options
	HTTP *http.Client
}

// New 创建 AI 识别客户端；HTTP 留空时用带超时的默认客户端。
func New(o Options, hc *http.Client) *Client {
	if hc == nil {
		t := o.Timeout
		if t <= 0 {
			t = 30 * time.Second
		}
		hc = &http.Client{Timeout: t}
	}
	if o.MaxAudioBytes <= 0 {
		o.MaxAudioBytes = 6 << 20
	}
	return &Client{Opt: o, HTTP: hc}
}

// Ready 报告 AI 链路是否可用（有端点、有模型即可；有的自建网关不要 key）。
func (c *Client) Ready() bool {
	return c != nil && c.Opt.Ready()
}

const systemPrompt = `你是音乐元数据助手。用户会给你一首歌的线索（文件名、已有标签、时长、歌词片段，可能还有音频片段）。
请判断它的原始发行信息，并只返回一个 JSON 对象，不要任何解释文字：
{"title":"歌名","artist":"歌手","album":"专辑（不确定就留空）","confidence":0.0到1.0,"reason":"一句话依据"}
规则：不确定的字段留空字符串；不要编造专辑名；歌名和歌手用最常见的官方写法（中文歌用中文，外文歌用原文）。`

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type audioPart struct {
	Type       string `json:"type"` // input_audio
	InputAudio struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	} `json:"input_audio"`
}

// Ask 让模型判断这首歌。allowAudio=false 或格式不支持时只发文本线索。
func (c *Client) Ask(ctx context.Context, h Hints, allowAudio bool) (Candidate, error) {
	if !c.Ready() {
		return Candidate{}, fmt.Errorf("AI 识别未配置")
	}

	prompt := buildPrompt(h)
	content := any(prompt)

	audioNote := ""
	if allowAudio && c.Opt.AllowAudio {
		if data, format, err := c.readClip(h); err == nil && data != "" {
			var ap audioPart
			ap.Type = "input_audio"
			ap.InputAudio.Data = data
			ap.InputAudio.Format = format
			content = []any{textPart{Type: "text", Text: prompt}, ap}
			audioNote = "（含音频片段）"
		}
	}

	body := map[string]any{
		"model":       c.Opt.Model,
		"temperature": 0,
		"messages": []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: content},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Candidate{}, err
	}

	url := strings.TrimRight(c.Opt.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Candidate{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Opt.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.Opt.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Candidate{}, fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return Candidate{}, fmt.Errorf("AI 接口返回 %d: %s", resp.StatusCode, snippet(string(data), 160))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Candidate{}, fmt.Errorf("AI 响应不是合法 JSON: %w", err)
	}
	if len(out.Choices) == 0 {
		return Candidate{}, fmt.Errorf("AI 没有返回内容")
	}

	cand, err := parseModelJSON(out.Choices[0].Message.Content)
	if err != nil {
		return Candidate{}, err
	}
	if cand.Source == "" {
		cand.Source = "ai-text"
		if audioNote != "" {
			cand.Source = "ai-audio"
		}
	}
	if cand.Confidence <= 0 {
		cand.Confidence = 0.6
	}
	return cand, nil
}

func buildPrompt(h Hints) string {
	var b strings.Builder
	b.WriteString("请识别这首歌：\n")
	fmt.Fprintf(&b, "- 文件名：%s\n", h.Name)
	if h.Ext != "" {
		fmt.Fprintf(&b, "- 格式：%s\n", strings.TrimPrefix(h.Ext, "."))
	}
	if h.Title != "" || h.Artist != "" || h.Album != "" {
		fmt.Fprintf(&b, "- 现有标签：title=%q artist=%q album=%q\n", h.Title, h.Artist, h.Album)
	} else {
		b.WriteString("- 现有标签：无（只有文件名）\n")
	}
	if h.Duration > 0 {
		fmt.Fprintf(&b, "- 时长：%.0f 秒\n", h.Duration)
	}
	if s := strings.TrimSpace(h.LyricSnippet); s != "" {
		fmt.Fprintf(&b, "- 歌词片段：\n%s\n", snippet(s, 400))
	}
	return b.String()
}

// readClip 读文件开头的一段交给模型。整段发不划算，也容易超限，
// 因此按 MaxAudioBytes 截断（mp3 帧是顺序的，截断不影响开头可解码）。
func (c *Client) readClip(h Hints) (string, string, error) {
	format := strings.ToLower(strings.TrimPrefix(h.Ext, "."))
	switch format {
	case "mp3", "wav", "flac", "m4a", "mp4", "ogg", "webm":
	default:
		return "", "", fmt.Errorf("格式 %s 不支持直接送给模型", format)
	}
	if format == "m4a" || format == "mp4" {
		format = "mp4"
	}
	if h.Path == "" {
		return "", "", fmt.Errorf("没有可读取的文件路径")
	}
	f, err := os.Open(h.Path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	limit := c.Opt.MaxAudioBytes
	if st.Size() < limit {
		limit = st.Size()
	}
	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", "", err
	}
	if n < 1024 {
		return "", "", fmt.Errorf("文件太小，跳过音频上传")
	}
	return base64.StdEncoding.EncodeToString(buf[:n]), format, nil
}

// parseModelJSON 从模型输出里抠出 JSON：常被包在 ```json 里，或前后带说明文字。
func parseModelJSON(s string) (Candidate, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '{'); i >= 0 {
		if j := strings.LastIndexByte(s, '}'); j > i {
			s = s[i : j+1]
		}
	}
	var raw struct {
		Title      string  `json:"title"`
		Artist     string  `json:"artist"`
		Album      string  `json:"album"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Candidate{}, fmt.Errorf("模型返回的内容解析失败: %w", err)
	}
	if strings.TrimSpace(raw.Title) == "" && strings.TrimSpace(raw.Artist) == "" {
		return Candidate{}, fmt.Errorf("模型没能给出歌名或歌手")
	}
	return Candidate{
		Title:      strings.TrimSpace(raw.Title),
		Artist:     strings.TrimSpace(raw.Artist),
		Album:      strings.TrimSpace(raw.Album),
		Confidence: raw.Confidence,
		Note:       strings.TrimSpace(raw.Reason),
	}, nil
}

func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Ping 用一次极小的请求验证端点/Key/模型名是否可用，不消耗多少额度。
func (c *Client) Ping(ctx context.Context) error {
	if !c.Ready() {
		return fmt.Errorf("AI 识别未配置")
	}
	body := map[string]any{
		"model":      c.Opt.Model,
		"max_tokens": 8,
		"messages":   []chatMessage{{Role: "user", Content: "ping，请只回复 ok"}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := strings.TrimRight(c.Opt.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Opt.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.Opt.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("接口返回 %d: %s", resp.StatusCode, snippet(string(data), 140))
	}
	return nil
}

// AudioFormats 用于前端提示哪些格式可以走音频识别。
func AudioFormats() []string {
	return []string{"mp3", "wav", "flac", "m4a", "mp4", "ogg", "webm"}
}
