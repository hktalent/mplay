package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hktalent/mplay/internal/config"
	"github.com/hktalent/mplay/internal/media"
)

// 这些测试锁住 v1.4.1 收紧的两条规则：
//  1. 没设访问令牌时，经反代/公网进来的请求一律 403（监听回环 + 反代对外是危险组合）；
//  2. 写方法必须同源，挡住第三方网页借浏览器打本机接口。
func testServer(t *testing.T, token string) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{Addr: "127.0.0.1:8737", AuthToken: token, MediaDirs: []string{filepath.Join(dir, "Music")}}
	return &Server{o: Options{Cfg: cfg, Version: "test"}}
}

func guarded() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func do(s *Server, method, target string, hdr map[string]string, remote string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.auth(guarded()).ServeHTTP(rec, r)
	return rec
}

func TestNoTokenRejectsProxiedRequests(t *testing.T) {
	s := testServer(t, "")
	// 本机直连：放行
	if rec := do(s, http.MethodGet, "/api/config", nil, "127.0.0.1:5000"); rec.Code != http.StatusOK {
		t.Fatalf("本机直连应放行，实际 %d", rec.Code)
	}
	// 反代（RemoteAddr 看似回环，但带 X-Forwarded-For）：拒绝
	for _, path := range []string{"/api/config", "/api/library", "/media/abc", "/cover/abc"} {
		rec := do(s, http.MethodGet, path, map[string]string{"X-Forwarded-For": "203.0.113.9"}, "127.0.0.1:5000")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("反代访问 %s 应 403，实际 %d", path, rec.Code)
		}
	}
	// 公网直连（监听 0.0.0.0 的场景）：拒绝
	if rec := do(s, http.MethodGet, "/api/config", nil, "203.0.113.9:5000"); rec.Code != http.StatusForbidden {
		t.Fatalf("公网直连应 403，实际 %d", rec.Code)
	}
	// 健康检查保持开放，便于监控
	if rec := do(s, http.MethodGet, "/api/health", nil, "203.0.113.9:5000"); rec.Code != http.StatusOK {
		t.Fatalf("健康检查应保持开放，实际 %d", rec.Code)
	}
}

func TestTokenRequiredEverywhere(t *testing.T) {
	s := testServer(t, "s3cret")
	if rec := do(s, http.MethodGet, "/api/library", nil, "127.0.0.1:5000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("设了令牌后无令牌应 401，实际 %d", rec.Code)
	}
	if rec := do(s, http.MethodGet, "/api/library", map[string]string{"X-Mplay-Token": "s3cret"}, "203.0.113.9:5000"); rec.Code != http.StatusOK {
		t.Fatalf("带正确令牌应放行，实际 %d", rec.Code)
	}
	// 令牌放 query 也要能过（媒体流/封面用这种方式）
	if rec := do(s, http.MethodGet, "/media/abc?token=s3cret", nil, "203.0.113.9:5000"); rec.Code != http.StatusOK {
		t.Fatalf("query token 应放行，实际 %d", rec.Code)
	}
	if rec := do(s, http.MethodGet, "/media/abc?token=wrong", nil, "203.0.113.9:5000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误令牌应 401，实际 %d", rec.Code)
	}
}

func TestWriteRequiresSameOrigin(t *testing.T) {
	s := testServer(t, "")
	hdr := map[string]string{"Origin": "https://evil.example"}
	if rec := do(s, http.MethodPost, "/api/config", hdr, "127.0.0.1:5000"); rec.Code != http.StatusForbidden {
		t.Fatalf("外站 Origin 的写请求应 403，实际 %d", rec.Code)
	}
	if rec := do(s, http.MethodGet, "/api/config", hdr, "127.0.0.1:5000"); rec.Code != http.StatusOK {
		t.Fatalf("读请求不该被 Origin 规则影响，实际 %d", rec.Code)
	}
}

func TestConfigRedactionForRemote(t *testing.T) {
	s := testServer(t, "")
	cfg := s.o.Cfg
	cfg.MediaDirs = []string{"/Users/someone/Music"}
	// 本机：暴露绝对路径
	r := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	if got := s.publicConfig(isLocal(r))["configPath"]; got != cfg.Path() {
		t.Fatalf("本机应看到配置路径，实际 %v", got)
	}
	// 远程：路径隐藏，目录只留名字
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	out := s.publicConfig(isLocal(r))
	if out["configPath"] != "" {
		t.Fatalf("远程不应看到配置路径，实际 %v", out["configPath"])
	}
	dirs, _ := out["mediaDirs"].([]string)
	if len(dirs) != 1 || dirs[0] != "Music" {
		t.Fatalf("远程只应看到目录名，实际 %v", out["mediaDirs"])
	}
}

func TestEntryRedaction(t *testing.T) {
	e := media.Entry{Path: "/Users/someone/Music/a.mp3", Dir: "/Users/someone/Music", Name: "a.mp3"}
	if out := redactEntry(e, false); out.Path != "" || out.Dir != "" || out.Name != "a.mp3" {
		t.Fatalf("远程条目应清掉路径但保留文件名: %+v", out)
	}
	if out := redactEntry(e, true); out.Path != e.Path {
		t.Fatalf("本机条目应保留完整路径: %+v", out)
	}
}
