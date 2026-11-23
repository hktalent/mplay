package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 假 LRCLIB：按 track_name/artist_name 组合返回固定记录，用来验证「选哪一条」。
func fakeLRCLIB(t *testing.T, table map[string][]Record) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if s := q.Get("q"); s != "" {
			// 自由文本搜索：这里用不到，返回空即可
			_, _ = w.Write([]byte(`[]`))
			return
		}
		key := q.Get("track_name") + "\x00" + q.Get("artist_name")
		recs := table[key]
		_ = json.NewEncoder(w).Encode(recs)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func synced(name, artist string, id int64) Record {
	return Record{ID: id, TrackName: name, ArtistName: artist, AlbumName: "专辑", Duration: 260,
		SyncedLyrics: "[00:01.00]" + name + "\n[00:05.00]第二句\n[00:09.00]第三句"}
}

// 网友上传的「反着写」条目：track=艺人、artist=歌名，权重必须被压下去。
func TestBestMatchRejectsSwappedJunk(t *testing.T) {
	table := map[string][]Record{
		"晴天\x00周杰伦": {synced("晴天", "周杰伦", 1), synced("周杰伦", "晴天", 2)},
		"周杰伦\x00晴天": {synced("周杰伦", "晴天", 2)},
	}
	srv := fakeLRCLIB(t, table)
	c := NewClient(srv.URL, t.TempDir(), true)
	m, err := c.BestMatch(context.Background(), []Variant{
		{Title: "晴天", Artist: "周杰伦"},
		{Title: "周杰伦", Artist: "晴天"},
	}, "", 0)
	if err != nil {
		t.Fatalf("应该匹配到正常条目: %v", err)
	}
	if m.Record.TrackName != "晴天" || m.Record.ArtistName != "周杰伦" {
		t.Fatalf("选中了脏条目: %+v", m.Record)
	}
	if m.TitleSim < 0.9 || m.ArtistSim < 0.9 {
		t.Fatalf("相似度不对: %+v", m)
	}
}

// 艺人完全不匹配（同名的翻唱/别的歌手版本）不能算命中。
func TestBestMatchRejectsWrongArtist(t *testing.T) {
	rec := Record{ID: 9, TrackName: "成都", ArtistName: "陳一發兒", PlainLyrics: "让我掉下眼泪的"}
	table := map[string][]Record{
		"成都\x00赵雷": {rec},
		"成都\x00":   {rec}, // 只有歌名（没有 artist_name）时歌词库也会返回记录
	}
	srv := fakeLRCLIB(t, table)
	c := NewClient(srv.URL, t.TempDir(), true)
	if _, err := c.BestMatch(context.Background(), []Variant{{Title: "成都", Artist: "赵雷"}}, "", 0); err == nil {
		t.Fatal("艺人不对也应算未命中")
	}
	// 没给艺人时允许按歌名命中（脏文件名里只剩歌名的情况）
	m, err := c.BestMatch(context.Background(), []Variant{{Title: "成都"}}, "", 0)
	if err != nil || m.TitleOnly == false {
		t.Fatalf("只有歌名时应按歌名命中并标记 titleOnly: %v %+v", err, m)
	}
}

// 名字里带 MP3_160K 这种水印的条目要排到正常条目后面。
func TestBestMatchPenalizesNoisyName(t *testing.T) {
	clean := synced("Yellow", "Coldplay", 3)
	other := synced("Yellow", "Coldplay", 4)
	noisy := synced("Coldplay", "Yellow(MP3_160K)", 5)
	table := map[string][]Record{
		"Coldplay\x00Yellow": {noisy},
		"Yellow\x00Coldplay": {clean, other, synced("Yellow", "Coldplay", 6), synced("Yellow", "Coldplay", 7), synced("Yellow", "Coldplay", 8)},
	}
	srv := fakeLRCLIB(t, table)
	c := NewClient(srv.URL, t.TempDir(), true)
	m, err := c.BestMatch(context.Background(), []Variant{
		{Title: "Coldplay", Artist: "Yellow"},
		{Title: "Yellow", Artist: "Coldplay"},
	}, "", 0)
	if err != nil {
		t.Fatalf("应该命中: %v", err)
	}
	if m.Record.TrackName != "Yellow" || m.Record.ArtistName != "Coldplay" {
		t.Fatalf("选中了带水印的条目: %+v", m.Record)
	}
}

// ResolveBest：第一次会写缓存，第二次 force=false 直接读缓存（不再请求远端）。
func TestResolveBestCaches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode([]Record{synced("十年", "陈奕迅", 11)})
	}))
	defer srv.Close()
	cache := t.TempDir()
	c := NewClient(srv.URL, cache, true)

	doc, m, err := c.ResolveBest(context.Background(), "k1", []Variant{{Title: "十年", Artist: "陈奕迅"}}, "", 0, true, 0)
	if err != nil || doc == nil || len(doc.Cues) == 0 || m == nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	if _, err := os.Stat(c.CachePath("k1")); err != nil {
		t.Fatalf("没写缓存: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "lyrics")); err != nil {
		t.Fatalf("缓存目录结构不对: %v", err)
	}
	hits = 0
	doc2, _, err := c.ResolveBest(context.Background(), "k1", []Variant{{Title: "十年", Artist: "陈奕迅"}}, "", 0, false, 0)
	if err != nil || doc2 == nil || doc2.Source != "cache" {
		t.Fatalf("第二次应读缓存: %v %+v", err, doc2)
	}
	if hits != 0 {
		t.Fatalf("读缓存时不该再请求远端，实际 %d 次", hits)
	}
}

// 关闭歌词库时任何查询都不该发出去。
func TestBestMatchDisabled(t *testing.T) {
	c := NewClient("http://127.0.0.1:0", t.TempDir(), false)
	if _, err := c.BestMatch(context.Background(), []Variant{{Title: "十年", Artist: "陈奕迅"}}, "", 0); err != ErrNotFound {
		t.Fatalf("未启用时应返回 ErrNotFound，实际 %v", err)
	}
}
