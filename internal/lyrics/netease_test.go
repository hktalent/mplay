package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeNetease 造一个最小的网易云：搜索结果固定，歌词按 id 返回。
func fakeNetease(t *testing.T, songs []map[string]any, lyrics map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search/get/web", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code":   200,
			"result": map[string]any{"songs": songs},
		})
	})
	mux.HandleFunc("/api/song/lyric", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		lrc, ok := lyrics[id]
		if !ok {
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "lrc": map[string]any{"lyric": ""}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"lrc":  map[string]any{"lyric": lrc},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeNeteaseSong(id int64, name string, artists []string, durMs int64) map[string]any {
	var as []map[string]any
	for _, a := range artists {
		as = append(as, map[string]any{"name": a})
	}
	return map[string]any{
		"id": id, "name": name, "duration": durMs,
		"artists": as,
		"album":   map[string]any{"name": "测试专辑"},
	}
}

// LRCLIB 找不到时，网易云能兜住，并且返回带时间轴的歌词。
func TestNeteaseFallback(t *testing.T) {
	songs := []map[string]any{
		fakeNeteaseSong(557584888, "往后余生", []string{"马良"}, 196000),
		fakeNeteaseSong(1311319953, "往后余生", []string{"孙茜茹", "马良"}, 236000),
	}
	lrcs := map[string]string{
		"557584888":  "[00:16.50]在没风的地方找太阳\n[00:20.00]在你冷的地方做暖阳",
		"1311319953": "[00:18.00]在没风的地方找太阳",
	}
	ns := fakeNetease(t, songs, lrcs)
	// LRCLIB 指向一个永远返回空列表的假服务
	ls := fakeLRCLIB(t, map[string][]Record{})
	c := NewClient(ls.URL, t.TempDir(), true)
	c.Netease = true
	c.NeteaseBase = ns.URL

	m, err := c.BestMatch(context.Background(), []Variant{
		{Title: "往后余生", Artist: "孙茜茹&马良"},
		{Title: "孙茜茹&马良", Artist: "往后余生"},
	}, "", 196)
	if err != nil {
		t.Fatalf("应该由网易云兜底命中: %v", err)
	}
	if m.Record.Source != "netease" {
		t.Fatalf("来源应为 netease: %+v", m.Record)
	}
	if m.Record.TrackName != "往后余生" || !strings.Contains(m.Record.SyncedLyrics, "[00:16.50]") {
		t.Fatalf("记录内容不对: %+v", m.Record)
	}
	doc := DocFromRecord(m.Record)
	if doc.Source != "netease" || !doc.Synced || len(doc.Cues) == 0 {
		t.Fatalf("歌词文档没生成对: %+v", doc)
	}
}

// 网易云上的同名不同歌（时长差很多）不能借，避免串歌。
func TestNeteaseRejectsWrongDuration(t *testing.T) {
	songs := []map[string]any{fakeNeteaseSong(9001, "无问", []string{"毛不易"}, 260000)}
	ns := fakeNetease(t, songs, map[string]string{"9001": "[00:05.00]无问西东"})
	ls := fakeLRCLIB(t, map[string][]Record{})
	c := NewClient(ls.URL, t.TempDir(), true)
	c.Netease = true
	c.NeteaseBase = ns.URL

	// 本地文件 6 分钟（360s），记录 260s，差 100s：不该命中
	if _, err := c.BestMatch(context.Background(), []Variant{{Title: "无问", Artist: "邓伟标"}}, "", 360); err == nil {
		t.Fatal("时长差太多 + 艺人不对，不应命中")
	}
}

// 关掉 Netease 时不发请求（只走 LRCLIB）。
func TestNeteaseDisabled(t *testing.T) {
	ns := fakeNetease(t, []map[string]any{fakeNeteaseSong(1, "晴天", []string{"周杰伦"}, 269000)}, nil)
	ls := fakeLRCLIB(t, map[string][]Record{})
	c := NewClient(ls.URL, t.TempDir(), true)
	c.NeteaseBase = ns.URL // 故意不打开 Netease
	if _, err := c.BestMatch(context.Background(), []Variant{{Title: "晴天", Artist: "周杰伦"}}, "", 269); err == nil {
		t.Fatal("Netease 关掉时不应命中")
	}
}
