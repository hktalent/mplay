package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hktalent/mplay/internal/config"
	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
	"github.com/hktalent/mplay/internal/music"
	"github.com/hktalent/mplay/internal/netguard"
)

// Options 是服务端依赖，由 main 装配。
type Options struct {
	Cfg     *config.Config
	Lib     *media.Library
	Lyr     *lyrics.Client
	Subs    *lyrics.SubtitleClient
	Search  *music.Searcher
	Web     fs.FS
	Version string
	Desktop bool
}

// Server 是 HTTP 层：一个 mux 同时服务静态资源、API 与媒体流。
type Server struct {
	o     Options
	mux   *http.ServeMux
	start time.Time

	// cfgMu 串行化配置写入：配置对象在内存里共享，并发 POST 会互相覆盖。
	cfgMu sync.Mutex

	// 歌词全文索引（内存）与构建状态
	snippets   *lyrics.SnippetIndex
	idxMu      sync.Mutex
	idxBuiltAt int64

	// 后台自动缓存歌词：进度快照 + 唤醒/退出通道
	preMu   sync.Mutex
	pre     PrecacheStats
	preKick chan struct{}
	preStop chan struct{}
}

// New 注册所有路由。
func New(o Options) *Server {
	s := &Server{
		o:        o,
		mux:      http.NewServeMux(),
		start:    time.Now(),
		snippets: lyrics.NewSnippetIndex(),
		preKick:  make(chan struct{}, 1),
		preStop:  make(chan struct{}),
	}

	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("POST /api/config", s.handlePostConfig)
	s.mux.HandleFunc("GET /api/library", s.handleLibrary)
	s.mux.HandleFunc("POST /api/library/scan", s.handleScan)
	s.mux.HandleFunc("GET /api/track/{id}", s.handleTrack)
	s.mux.HandleFunc("POST /api/track/{id}/identify", s.handleIdentifyTrack)
	s.mux.HandleFunc("POST /api/library/identify", s.handleIdentifyLibrary)
	s.mux.HandleFunc("POST /api/ai/check", s.handleAICheck)
	s.mux.HandleFunc("POST /api/track/{id}/duration", s.handleDuration)
	s.mux.HandleFunc("GET /api/lyric/{id}", s.handleLyric)
	s.mux.HandleFunc("POST /api/lyric/{id}", s.handleLyricUpload)
	s.mux.HandleFunc("GET /api/lyric/{id}/candidates", s.handleLyricCandidates)
	s.mux.HandleFunc("GET /media/{id}", s.handleMedia)
	s.mux.HandleFunc("GET /cover/{id}", s.handleCover)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/search/lyrics", s.handleSearchLyrics)
	s.mux.HandleFunc("GET /api/remote/lyric", s.handleRemoteLyric)
	s.mux.HandleFunc("GET /api/sources", s.handleSources)
	s.mux.HandleFunc("POST /api/pick/folder", s.handlePickFolder)
	s.mux.HandleFunc("POST /api/open", s.handleOpen)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/cache/status", s.handleCacheStatus)
	s.mux.HandleFunc("POST /api/cache/run", s.handleCacheRun)
	s.mux.HandleFunc("/", s.handleStatic)

	go s.precacheWorker()
	if o.Lib != nil {
		o.Lib.OnScanned = s.KickPrecache // 每次扫描完成后自动补歌词
	}
	s.KickPrecache() // 启动后先排一轮：已经扫描完毕的曲库可以立刻开始补歌词

	return s
}

// Handler 返回给 http.Server 的最终 handler（带鉴权与安全头）。
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.auth(s.mux))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"img-src 'self' data: blob: https:",
			"media-src 'self' blob: https:",
			"connect-src 'self' https:",
			"style-src 'self' 'unsafe-inline'",
			"script-src 'self'",
			"font-src 'self' data:",
			"base-uri 'none'",
			"form-action 'self'",
		}, "; "))
		if r.URL.Path == "/api/health" {
			h.Set("Access-Control-Allow-Origin", "*")
		}
		next.ServeHTTP(w, r)
	})
}

// ctxKeyLocal 记录这一轮请求是不是「本机直连」，供各 handler 判断能否
// 暴露绝对路径、能否调用本机 GUI 动作。
type ctxKey int

const ctxKeyLocal ctxKey = iota

// isLocal 从 context 取本机直连判定；没有上下文时退回实时判断。
func isLocal(r *http.Request) bool {
	if v, ok := r.Context().Value(ctxKeyLocal).(bool); ok {
		return v
	}
	return netguard.IsLocalRequest(r)
}

// auth 是门禁，规则（v1.4.1 收紧）：
//   - 设了访问令牌：/api、/media、/cover 一律校验令牌（本机也一样）；
//   - 没设令牌：只放行「本机直连」的请求，经反代/公网进来的一律 403。
//     这一条专门对付「监听 127.0.0.1 + HTTPS 反代对外」这种部署：
//     反代会把 RemoteAddr 改写成 127.0.0.1，只有代理头能识别真实来源。
//   - 写方法额外校验同源，挡住第三方网页借浏览器打本机接口（CSRF）。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local := netguard.IsLocalRequest(r)
		p := r.URL.Path
		guarded := strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/media/") || strings.HasPrefix(p, "/cover/")
		token := strings.TrimSpace(s.o.Cfg.AuthToken)
		if guarded && p != "/api/health" {
			if token != "" {
				given := tokenFrom(r)
				if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
					writeJSON(w, http.StatusUnauthorized, map[string]any{
						"error": "需要访问令牌", "authRequired": true, "localRequest": local,
					})
					return
				}
			} else if !local {
				writeJSON(w, http.StatusForbidden, map[string]any{
					"error":        "此实例未设置访问令牌，已拒绝来自公网/反向代理的请求",
					"authRequired": true,
					"localRequest": false,
					"hint":         "在服务器上打开设置 → 访问令牌，设一个令牌后用 https://m.51pwn.com/?token=你的令牌 访问",
				})
				return
			}
			if isUnsafeMethod(r.Method) && !netguard.SameOrigin(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{
					"error": "拒绝跨站请求（Origin 与本站不一致）", "localRequest": local,
				})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyLocal, local)))
	})
}

func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// tokenFrom 依次从请求头、查询参数、Cookie 取访问令牌。
func tokenFrom(r *http.Request) string {
	if v := r.Header.Get("X-Mplay-Token"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("token"); v != "" {
		return v
	}
	if c, err := r.Cookie("mplay_token"); err == nil {
		return c.Value
	}
	return ""
}

// handleStatic 服务 go:embed 进来的 web 目录。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.o.Web, name)
	if err != nil {
		// 前端是 hash 路由，未知静态路径统一 404，避免掩盖真实缺文件。
		http.NotFound(w, r)
		return
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	switch path.Ext(name) {
	case ".js":
		ctype = "text/javascript; charset=utf-8"
	case ".css":
		ctype = "text/css; charset=utf-8"
	case ".svg":
		ctype = "image/svg+xml"
	case ".html":
		ctype = "text/html; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// writeJSON 统一 JSON 输出。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("[mplay] 写响应失败: %v", err)
	}
}

func errJSON(w http.ResponseWriter, status int, err error, hint string) {
	body := map[string]any{"error": err.Error()}
	if hint != "" {
		body["hint"] = hint
	}
	writeJSON(w, status, body)
}

// decodeBody 限制体积解析 JSON，防恶意超大请求。
func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(v)
}

func (s *Server) desktopEnabled() bool { return s.o.Desktop }
