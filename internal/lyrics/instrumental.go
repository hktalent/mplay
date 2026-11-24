package lyrics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrInstrumental 是「歌词库明确说这首歌没有词」的结论（酷狗对纯音乐返回
// “纯音乐，请欣赏”这类占位文本）。它不是失败：既不该算命中，也不该反复重试。
var ErrInstrumental = errors.New("纯音乐，无歌词")

// NoLyricNote 是给前端/曲库看的固定说法。
const NoLyricNote = "无歌词，纯音乐"

// 占位结论的几种固定说法。真歌词里也可能出现「纯音乐」三个字，
// 所以「纯音乐」必须和「欣赏」一起出现才算，不能只看一个词。
var (
	noLyricExact = []string{"暂无歌词", "无歌词", "没有歌词", "歌词正在上传", "该歌曲没有歌词"}
	// 占位文本常常是带时间轴的 LRC（"[00:01.58]纯音乐，请欣赏"），
	// 判断前先把时间标签和 [ti:]/[total:] 这类元信息剥掉。
	lrcTimeRe    = regexp.MustCompile(`\[\d{1,3}:\d{1,2}(?::\d{1,2})?(?:[.,]\d{1,3})?\]`)
	lrcMetaRe    = regexp.MustCompile(`(?m)^\[[a-zA-Z]{1,10}:[^\]]*\]\s*$`)
	punctStripRe = regexp.MustCompile(`[\s，,。.、；;：:！!？?（）()\[\]【】"'“”…]+`)
)

// isInstrumentalText 判断一段“歌词”其实是不是占位结论。
// 真歌词少则几十字多则几百字，占位结论通常只有一行，所以先按长度卡一道，
// 避免把真歌词里偶尔出现的“纯音乐”三个字误判。
func isInstrumentalText(s string) bool {
	s = htmlTag.ReplaceAllString(s, " ")
	s = lrcMetaRe.ReplaceAllString(s, "")
	s = lrcTimeRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// 占位结论都是很短的一句；真歌词一行也有十几个字，先按长度卡一道。
	if len([]rune(s)) > 24 {
		return false
	}
	core := punctStripRe.ReplaceAllString(s, "")
	low := strings.ToLower(core)
	// 「纯音乐」单独成句也算（酷狗有时只给这三个字）。
	if core == "纯音乐" || (strings.Contains(core, "纯音乐") && strings.Contains(core, "欣赏")) {
		return true
	}
	for _, h := range noLyricExact {
		if strings.Contains(core, h) {
			return true
		}
	}
	return strings.Contains(low, "instrumental")
}

// NoLyric 是「这首歌没有歌词」的持久化结论。
type NoLyric struct {
	Reason string `json:"reason"` // instrumental | empty
	Note   string `json:"note"`
	Source string `json:"source,omitempty"`
	At     int64  `json:"at"`
}

// NoLyricPath 是结论文件路径（与歌词缓存同目录，后缀不同，互不影响）。
func (c *Client) NoLyricPath(key string) string {
	return filepath.Join(c.CacheDir, "lyrics", key+".nolyric.json")
}

// ReadNoLyric 读「没有歌词」的结论。
func (c *Client) ReadNoLyric(key string) (*NoLyric, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	data, err := os.ReadFile(c.NoLyricPath(key))
	if err != nil {
		return nil, ErrNotFound
	}
	var n NoLyric
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, ErrNotFound
	}
	if n.Reason == "" {
		n.Reason = "instrumental"
	}
	return &n, nil
}

// WriteNoLyric 落盘「没有歌词」的结论（原子写）。
func (c *Client) WriteNoLyric(key string, n NoLyric) error {
	if key == "" {
		return nil
	}
	if n.Reason == "" {
		n.Reason = "instrumental"
	}
	if n.Note == "" {
		n.Note = NoLyricNote
	}
	if n.At == 0 {
		n.At = time.Now().Unix()
	}
	dir := filepath.Join(c.CacheDir, "lyrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	tmp := c.NoLyricPath(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	rename := os.Rename(tmp, c.NoLyricPath(key))
	if rename != nil {
		os.Remove(tmp)
	}
	return rename
}

// InstrumentalDoc 生成一份“没有歌词”的文档：不带 cue，只带结论，
// 让前端可以显示「无歌词，纯音乐」而不是一行假歌词。
func InstrumentalDoc(title, artist string) *Doc {
	return &Doc{
		Format:       "none",
		Synced:       false,
		Source:       "instrumental",
		Instrumental: true,
		Note:         NoLyricNote,
		Title:        title,
		Artist:       artist,
		Meta:         map[string]string{"instrumental": "1"},
	}
}

// MigrateNoLyricCache 把历史上被当成歌词存下来的占位文本（“纯音乐，请欣赏”）
// 改成「没有歌词」结论：删掉歌词缓存、写结论文件。返回处理条数。
func (c *Client) MigrateNoLyricCache() int {
	dir := filepath.Join(c.CacheDir, "lyrics")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	fixed := 0
	for _, de := range ents {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".nolyric.json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc Doc
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		if !c.placeholderDoc(&doc) {
			continue
		}
		_ = c.WriteNoLyric(key, NoLyric{Reason: "instrumental", Source: doc.Source, Note: NoLyricNote})
		_ = os.Remove(path)
		fixed++
	}
	return fixed
}

// placeholderDoc 判断缓存里的文档是不是“整篇都是占位文本”。
func (c *Client) placeholderDoc(doc *Doc) bool {
	if doc == nil || len(doc.Cues) == 0 {
		return false
	}
	texts := 0
	for _, cue := range doc.Cues {
		t := strings.TrimSpace(cue.Text)
		if t == "" {
			continue
		}
		texts++
		if !isInstrumentalText(t) {
			return false
		}
	}
	return texts > 0
}
