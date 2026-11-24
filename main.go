// mplay —— 跨平台（macOS / Linux / Windows）的音视频播放器：
// 本地曲库自动配歌词/字幕、播放时逐行同步，同一套代码既能跑成网页，
// 也能被 urlbox 包成桌面应用。
//
// 设计取舍（为什么是「本地服务 + WebView」而不是原生播放器）：
//  1. 播放能力交给系统 WebView 的 <audio>/<video>，三种平台共用一份渲染逻辑；
//  2. 歌词同步、字幕叠层、搜索界面全部是前端代码，改一次三端同时生效；
//  3. Go 负责文件系统、编码识别、远端歌词抓取这些浏览器做不到的事；
//  4. 部署时它就是普通 HTTP 服务（同一份二进制），桌面时监听回环地址后拉 urlbox。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hktalent/mplay/internal/config"
	"github.com/hktalent/mplay/internal/desktop"
	"github.com/hktalent/mplay/internal/lyrics"
	"github.com/hktalent/mplay/internal/media"
	"github.com/hktalent/mplay/internal/music"
	"github.com/hktalent/mplay/internal/server"
)

// version 可在构建时用 -ldflags "-X main.version=..." 覆盖。
var version = "1.3.0"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, string(os.PathListSeparator)) }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var (
		mediaDirs   stringList
		addrFlag    = flag.String("addr", "", "监听地址，例如 127.0.0.1:8737 或 :8737（服务端部署用后者）")
		desktopMode = flag.Bool("desktop", false, "以桌面模式启动：只监听回环地址，并用 urlbox 打开窗口")
		openFlag    = flag.Bool("open", false, "启动后自动用系统浏览器打开")
		lanFlag     = flag.Bool("lan", false, "允许局域网访问（默认只监听回环）")
		cfgPath     = flag.String("config", "", "配置文件路径（默认 ~/.config/mplay/config.json）")
		cachePath   = flag.String("cache", "", "缓存目录（默认 ~/.cache/mplay）")
		noScan      = flag.Bool("no-scan", false, "启动时先不扫描曲库")
		showVersion = flag.Bool("version", false, "打印版本后退出")
	)
	flag.Var(&mediaDirs, "media", "媒体目录，可重复指定；与配置里的目录合并")
	flag.Parse()

	if *showVersion {
		fmt.Println("mplay " + version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("[mplay] %v", err)
	}
	cfg.ApplyEnv()
	if *addrFlag != "" {
		cfg.Addr = *addrFlag
	}
	if len(mediaDirs) > 0 {
		cfg.MediaDirs = append(cfg.MediaDirs, mediaDirs...)
		cfg.Normalize()
	}
	if *lanFlag {
		cfg.AllowLAN = true
	}
	// 桌面模式只监听回环；服务端模式尊重配置文件里的地址（不擅自改成 8737）。
	if *desktopMode && *addrFlag == "" && !cfg.AllowLAN {
		cfg.Addr = "127.0.0.1:8737"
	}

	cacheDir := *cachePath
	if cacheDir == "" {
		cacheDir = config.CacheDir()
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Fatalf("[mplay] 创建缓存目录失败: %v", err)
	}

	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("[mplay] 内嵌前端资源损坏: %v", err)
	}

	lib := media.NewLibrary(cacheDir, func() []string { return cfg.MediaDirs })
	lyr := lyrics.NewClient(cfg.LRCLIBBase, cacheDir, cfg.EnableLRCLIB)
	lyr.Kugou = cfg.EnableKugou
	lyr.Netease = cfg.EnableNeteaseSource
	subs := lyrics.NewSubtitleClient(cfg.OSAPIKey, cfg.OSUsername, cfg.OSPassword, cacheDir)
	searcher := music.New(cfg)

	opts := server.Options{
		Cfg:     cfg,
		Lib:     lib,
		Lyr:     lyr,
		Subs:    subs,
		Search:  searcher,
		Web:     webFS,
		Version: version,
		Desktop: *desktopMode || config.IsDesktop(),
	}
	// 老版本把酷狗返回的「纯音乐，请欣赏」当成歌词存了下来，启动时先清一遍，
	// 免得界面里挂着假歌词、统计也虚高。
	if n := lyr.MigrateNoLyricCache(); n > 0 {
		log.Printf("[mplay] 修正了 %d 条纯音乐占位歌词缓存", n)
	}

	srv := server.New(opts)

	// 先 Listen 再起服务：桌面模式端口被占用时要顺延，同时才能拿到真实端口。
	ln, actualAddr, err := listen(cfg.Addr)
	if err != nil {
		log.Fatalf("[mplay] 监听 %s 失败: %v", cfg.Addr, err)
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(os.Stderr, "[http] ", log.LstdFlags),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !*noScan {
		go func() {
			if err := lib.Scan(ctx); err != nil {
				log.Printf("[mplay] 扫描曲库失败: %v", err)
			} else {
				st := lib.Stats()
				log.Printf("[mplay] 曲库就绪：音频 %v / 视频 %v / 有歌词 %v", st["audio"], st["video"], st["withLyric"])
			}
		}()
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[mplay] HTTP 服务退出: %v", err)
			stop()
		}
	}()

	url := desktop.LoopbackURL(actualAddr)
	log.Printf("[mplay] v%s 已启动：%s", version, url)
	log.Printf("[mplay] 配置: %s", cfg.Path())
	log.Printf("[mplay] 缓存: %s", cacheDir)
	if n := len(cfg.MediaDirs); n > 0 {
		log.Printf("[mplay] 媒体目录 %d 个", n)
	} else {
		log.Printf("[mplay] 尚未配置媒体目录：用 -media <目录> 启动，或在网页「设置」里添加")
	}

	done := make(chan struct{})
	if *desktopMode || config.IsDesktop() {
		if cmd, err := desktop.Launch(url); err == nil {
			log.Printf("[mplay] 已用 urlbox 打开窗口（pid=%d）", cmd.Process.Pid)
			go func() {
				_ = cmd.Wait()
				close(done)
			}()
		} else {
			log.Printf("[mplay] 未找到 urlbox（%v），改用系统浏览器", err)
			_ = desktop.OpenBrowser(url)
		}
	} else if *openFlag {
		_ = desktop.OpenBrowser(url)
	}

	select {
	case <-ctx.Done():
	case <-done:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Printf("[mplay] 已退出")
}

// listen 绑定地址：端口被占用时顺延最多 10 个端口（桌面模式常见）。
func listen(addr string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, ln.Addr().String(), nil
	}
	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, "", err
	}
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	if port == 0 {
		return nil, "", err
	}
	for i := 1; i <= 10; i++ {
		tryAddr := net.JoinHostPort(host, fmt.Sprint(port+i))
		if l2, e2 := net.Listen("tcp", tryAddr); e2 == nil {
			log.Printf("[mplay] %s 已被占用，改用 %s", addr, tryAddr)
			return l2, l2.Addr().String(), nil
		}
	}
	return nil, "", err
}
