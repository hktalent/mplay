package identify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hktalent/mplay/internal/lyrics"
)

func TestLocalCandidatesPreferTags(t *testing.T) {
	h := Hints{Name: "01. 某资源站 - 夜空中最亮的星 [320k].mp3", Title: "夜空中最亮的星", Artist: "逃跑计划"}
	cands := LocalCandidates(h)
	if len(cands) == 0 {
		t.Fatal("应该有候选")
	}
	if cands[0].Source != "tag" || cands[0].Title != "夜空中最亮的星" {
		t.Fatalf("标签候选应排第一，实际 %+v", cands[0])
	}
	// 文件名解析也要给出一份（万一标签是错的）
	var hasFilename bool
	for _, c := range cands {
		if c.Source == "filename" {
			hasFilename = true
		}
	}
	if !hasFilename {
		t.Fatalf("缺少文件名候选: %+v", cands)
	}
}

func TestLocalCandidatesFilenameOnly(t *testing.T) {
	cands := LocalCandidates(Hints{Name: "逃跑计划 - 夜空中最亮的星.mp3"})
	if len(cands) < 2 {
		t.Fatalf("文件名应给出正反两种顺序，实际 %+v", cands)
	}
	if cands[0].Title == "" || cands[0].Artist == "" {
		t.Fatalf("首选候选应有歌名和艺人: %+v", cands[0])
	}
}

func TestParseModelJSONToleratesFences(t *testing.T) {
	c, err := parseModelJSON("好的，结果如下：\n```json\n{\"title\":\"夜空中最亮的星\",\"artist\":\"逃跑计划\",\"confidence\":0.82}\n```\n以上。")
	if err != nil {
		t.Fatalf("应能解析被包裹的 JSON: %v", err)
	}
	if c.Title != "夜空中最亮的星" || c.Artist != "逃跑计划" || c.Confidence != 0.82 {
		t.Fatalf("解析结果不对: %+v", c)
	}
	if _, err := parseModelJSON("抱歉我无法判断"); err == nil {
		t.Fatal("没有 JSON 时应报错")
	}
}

func TestAskTextMode(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		if a := r.Header.Get("Authorization"); a != "Bearer test-key" {
			t.Errorf("Authorization 不对: %q", a)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"夜空中最亮的星\",\"artist\":\"逃跑计划\",\"album\":\"世界\"}"}}]}`))
	}))
	defer srv.Close()

	c := New(Options{Enabled: true, BaseURL: srv.URL + "/v1", APIKey: "test-key", Model: "m1", AllowPrivate: true}, srv.Client())
	cand, err := c.Ask(context.Background(), Hints{Name: "abc.mp3"}, false)
	if err != nil {
		t.Fatalf("识别失败: %v", err)
	}
	if cand.Title != "夜空中最亮的星" || cand.Artist != "逃跑计划" {
		t.Fatalf("解析结果不对: %+v", cand)
	}
	if cand.Source != "ai-text" {
		t.Fatalf("来源应为 ai-text，实际 %q", cand.Source)
	}
	// 不允许音频时，请求体里不能出现 input_audio
	raw, _ := json.Marshal(gotBody)
	if strings.Contains(string(raw), "input_audio") {
		t.Fatalf("未允许音频却上传了音频: %s", raw)
	}
}

func TestAskAudioMode(t *testing.T) {
	f := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(f, append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), make([]byte, 4096)...), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"T\",\"artist\":\"A\"}"}}]}`))
	}))
	defer srv.Close()

	c := New(Options{Enabled: true, BaseURL: srv.URL + "/v1", Model: "m1", AllowAudio: true, AllowPrivate: true}, srv.Client())
	if _, err := c.Ask(context.Background(), Hints{Name: "song.mp3", Path: f, Ext: ".mp3"}, true); err != nil {
		t.Fatalf("音频模式识别失败: %v", err)
	}
	raw, _ := json.Marshal(gotBody)
	if !strings.Contains(string(raw), "input_audio") {
		t.Fatalf("允许音频时应带上 input_audio: %s", raw)
	}
	if !strings.Contains(string(raw), `"format":"mp3"`) {
		t.Fatalf("音频格式应为 mp3: %s", raw)
	}
}

func TestAskDisabledOrUnauthorized(t *testing.T) {
	c := New(Options{Enabled: false, BaseURL: "http://x/v1", Model: "m"}, nil)
	if _, err := c.Ask(context.Background(), Hints{Name: "a.mp3"}, false); err == nil {
		t.Fatal("未启用时应返回错误")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	c2 := New(Options{Enabled: true, BaseURL: srv.URL + "/v1", Model: "m", AllowPrivate: true}, srv.Client())
	_, err := c2.Ask(context.Background(), Hints{Name: "a.mp3"}, false)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("应把 401 报出来，实际 %v", err)
	}
}

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	c := New(Options{Enabled: true, BaseURL: srv.URL + "/v1", Model: "m", AllowPrivate: true}, srv.Client())
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping 失败: %v", err)
	}
}

// Verify 在没有歌词库客户端时必须降级为「未校验」，而不是报错或盲信。
func TestVerifyWithoutLyricsClient(t *testing.T) {
	cand := Candidate{Title: "T", Artist: "A", Source: "filename"}
	out, ok := Verify(context.Background(), nil, cand, 0)
	if ok {
		t.Fatal("没有歌词库时不应标记为已校验")
	}
	if out.Title != "T" {
		t.Fatalf("不应改动候选内容: %+v", out)
	}
	off := &lyrics.Client{Enabled: false}
	if _, ok := Verify(context.Background(), off, cand, 0); ok {
		t.Fatal("歌词库关闭时不应标记为已校验")
	}
}

// score 的基本倾向：歌名完全一致 > 相似 > 无关。
func TestScorePrefersExactTitle(t *testing.T) {
	cand := Candidate{Title: "夜空中最亮的星", Artist: "逃跑计划"}
	exact := lyrics.Record{TrackName: "夜空中最亮的星", ArtistName: "逃跑计划", Duration: 240}
	similar := lyrics.Record{TrackName: "夜空中最亮的星 (Live)", ArtistName: "逃跑计划", Duration: 240}
	other := lyrics.Record{TrackName: "稻香", ArtistName: "周杰伦", Duration: 240}

	if s1, s2, s3 := score(cand, exact, 240), score(cand, similar, 240), score(cand, other, 240); !(s1 > s2 && s2 > s3) {
		t.Fatalf("打分次序不对: %.3f %.3f %.3f", s1, s2, s3)
	}
}
