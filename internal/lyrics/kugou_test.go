package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeKugou 造一个最小的酷狗：搜索返回固定列表，歌词接口返回 base64 LRC。
func fakeKugou(t *testing.T, songs []map[string]any, lrc string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/song_search_v2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": 1,
			"data":   map[string]any{"total": len(songs), "lists": songs},
		})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": 200, "errcode": 0,
			"candidates": []map[string]any{{"id": "1", "accesskey": "k", "song": "x", "singer": "y", "duration": 166}},
		})
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status":  200,
			"content": base64.StdEncoding.EncodeToString([]byte(lrc)),
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func kugouFakeSong(name, singer, hash string, dur int64) map[string]any {
	return map[string]any{
		"SongName":   "<em>" + name + "</em>", // 酷狗真实返回带高亮标签
		"SingerName": "<em>" + singer + "</em>",
		"FileHash":   hash,
		"AlbumName":  "测试专辑",
		"Duration":   dur,
		"AlbumID":    "123", // 实测确实返回字符串
	}
}

// 酷狗兜底：歌名/歌手带 <em> 标签、艺人写法不完全一致（L（桃籽） vs 003L 桃籽）也要能命中。
func TestKugouFallback(t *testing.T) {
	songs := []map[string]any{
		kugouFakeSong("晚风作酒", "L（桃籽）、周林枫、三楠", "hashA", 166),
		kugouFakeSong("晚风作酒", "黄静美", "hashB", 205),
	}
	lrc := "[ti:晚风作酒]\n[ar:L（桃籽）]\n[00:12.00]晚风作酒\n[00:16.00]第二句"
	ks := fakeKugou(t, songs, lrc)
	ls := fakeLRCLIB(t, map[string][]Record{})
	c := NewClient(ls.URL, t.TempDir(), true)
	c.Kugou = true
	c.KugouBase = ks.URL

	m, err := c.BestMatch(context.Background(), []Variant{
		{Title: "晚风作酒", Artist: "003L 桃籽"},
		{Title: "003L 桃籽", Artist: "晚风作酒"},
	}, "", 166.7)
	if err != nil {
		t.Fatalf("应该由酷狗兜底命中: %v", err)
	}
	if m.Record.Source != "kugou" || m.Record.TrackName != "晚风作酒" {
		t.Fatalf("命中的记录不对（高亮标签没剥掉？）: %+v", m.Record)
	}
	if !strings.Contains(m.Record.SyncedLyrics, "[00:12.00]") {
		t.Fatalf("LRC 没解析出来: %+v", m.Record)
	}
	doc := DocFromRecord(m.Record)
	if doc.Source != "kugou" || len(doc.Cues) == 0 || doc.Title != "晚风作酒" {
		t.Fatalf("歌词文档不对: %+v", doc)
	}
}

// 时长差太多的同名歌（别的歌手版本）不能借。
func TestKugouRejectsWrongDuration(t *testing.T) {
	songs := []map[string]any{kugouFakeSong("无问", "毛不易", "hashC", 260)}
	ks := fakeKugou(t, songs, "[00:05.00]无问西东")
	ls := fakeLRCLIB(t, map[string][]Record{})
	c := NewClient(ls.URL, t.TempDir(), true)
	c.Kugou = true
	c.KugouBase = ks.URL
	if _, err := c.BestMatch(context.Background(), []Variant{{Title: "无问", Artist: "邓伟标"}}, "", 360); err == nil {
		t.Fatal("艺人不对 + 时长差 100 秒，不应命中")
	}
}
