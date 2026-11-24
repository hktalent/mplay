package server

import (
	"path/filepath"
	"strings"

	"github.com/hktalent/mplay/internal/config"
	"github.com/hktalent/mplay/internal/media"
)

// 这一组函数负责「给非本机直连的请求做脱敏」。
//
// 为什么需要：曲库条目里带着绝对路径（/Users/<用户名>/…），配置文件路径更是
// 直接把账号名和目录结构交出去。这些信息对本机打开网页的人是无害的、对
// 公网访客却是侦察情报，所以只在 isLocal 为真时原样返回。

// baseNames 把 ["/Users/a/Music", "/Users/a/音乐"] 变成 ["Music", "音乐"]。
func baseNames(dirs []string) []string {
	if dirs == nil {
		return nil
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = strings.TrimRight(d, `/\`)
		if d == "" {
			continue
		}
		b := filepath.Base(d)
		if b == "" || b == "." || b == string(filepath.Separator) {
			b = d
		}
		out = append(out, b)
	}
	return out
}

// redactEntry 返回条目的副本：远程访问时清掉绝对路径与同名侧车文件路径，
// 只留下文件名（文件名本身是曲库内容的一部分，界面需要它）。
func redactEntry(e media.Entry, local bool) media.Entry {
	if local {
		return e
	}
	e.Path = ""
	e.Dir = ""
	if len(e.Sidecars) > 0 {
		sc := make([]media.Sidecar, 0, len(e.Sidecars))
		for _, s := range e.Sidecars {
			s.Path = ""
			sc = append(sc, s)
		}
		e.Sidecars = sc
	}
	return e
}

// redactSources 返回音源列表副本：去掉自定义音源里的请求头，
// 那里通常放着自建曲库的 Authorization / Cookie。
func redactSources(src []config.Source) []config.Source {
	if src == nil {
		return nil
	}
	out := make([]config.Source, 0, len(src))
	for _, s := range src {
		if len(s.Headers) > 0 {
			s.Headers = nil
		}
		out = append(out, s)
	}
	return out
}
