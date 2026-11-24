// Package netguard 是 mplay 的网络边界：判断「这个请求是不是本机直连」，
// 以及「服务器要主动请求的这个 URL 是不是公网地址」。
//
// 背景（实测踩过的坑）：
//   - 服务器默认监听回环；一旦放到 HTTPS 反代后面（m.51pwn.com），
//     所有请求的 RemoteAddr 都变成 127.0.0.1，只判 RemoteAddr 会以为全是"本机"。
//   - 「打开本地文件夹」「用系统播放器打开」这类动作只应该在真正坐在
//     这台 Mac 前面的人才能触发，所以必须按请求来源判断，而不是按监听地址判断。
//   - 自定义音源 / AI 端点都是服务器主动出站，必须挡掉内网与环回地址，
//     否则就是一个 SSRF 探测/读取原语。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrPrivateTarget 表示目标 URL 指向内网、环回或链路本地地址。
var ErrPrivateTarget = errors.New("目标地址不允许（内网 / 环回 / 链路本地）")

// proxyHeaders 只要出现任意一个，就说明请求经过了反向代理，
// 不再当作「本机直连」——即使它的值写着 127.0.0.1 也不信。
var proxyHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
	"Forwarded",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"CF-Connecting-IP",
	"True-Client-IP",
}

// cgnat 等容易被忽略的保留段，这里显式列出（net.IP.IsPrivate 不含它们）。
var extraPrivate = mustCIDRs(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"198.18.0.0/15",
	"240.0.0.0/4",
	"64:ff9b::/96",
	"100::/64",
)

func mustCIDRs(list ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, s := range list {
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// IsPrivateIP 判断一个 IP 是否属于内网 / 环回 / 链路本地 / 保留段。
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range extraPrivate {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// requestIP 取请求的对端 IP。
func requestIP(r *http.Request) net.IP {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host)
}

// IsLocalRequest 判断请求是不是「真正在这台机器上、直连本机端口」的。
//
// 三个条件都满足才算本机直连：
//  1. 对端地址是回环；
//  2. 没有任何代理头（反代会把 RemoteAddr 变成 127.0.0.1，只能靠这个识别）；
//  3. 浏览器明确标注为跨站请求（Sec-Fetch-Site: cross-site）的一律不算。
func IsLocalRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	ip := requestIP(r)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, h := range proxyHeaders {
		if strings.TrimSpace(r.Header.Get(h)) != "" {
			return false
		}
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "cross-site", "same-site":
		return false
	}
	return true
}

// SameOrigin 判断「带 Origin/Referer 的写请求」是否来自本站。
// 没有 Origin 的请求（curl / 桌面外壳 / 同源表单）放行；
// 有 Origin 但不是本 Host 的，一律拒绝——防第三方网页借浏览器打本机接口。
func SameOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		// 没有 Origin：可能是 curl、桌面外壳、或同一浏览器发起的同源请求。
		// 仍然看 Sec-Fetch-Site，能标 cross-site 的直接拒。
		return strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) != "cross-site"
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := r.Host
	// 反代时 Host 通常是外部域名，Origin 也是同一个，直接比即可。
	if strings.EqualFold(u.Host, host) {
		return true
	}
	// 容忍端口差异之外的部分（协议由反代决定，不再比对）
	return false
}

// hostIsPrivate 解析主机名并判断是否落到内网。
// 字面 IP 直接判断；域名做一次解析，任意一个解析结果是内网就拒绝
// （挡住「DNS 指向 127.0.0.1」这类玩法，以及大部分 DNS rebinding）。
func hostIsPrivate(ctx context.Context, host string) bool {
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return IsPrivateIP(ip)
	}
	lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lctx, host)
	if err != nil || len(addrs) == 0 {
		// 解析不出来就不让请求（宁可失败，也别让它有机会落到内网）
		return true
	}
	for _, a := range addrs {
		if IsPrivateIP(a.IP) {
			return true
		}
	}
	return false
}

// CheckURL 校验服务器主动出站的目标：只允许 http/https 且必须是公网地址。
// 用于自定义音源、AI 端点这类由用户/接口配置决定的地址。
func CheckURL(ctx context.Context, raw string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("地址不合法: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("只允许 http/https 地址（当前 %q）", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("地址缺少主机名")
	}
	if hostIsPrivate(ctx, u.Hostname()) {
		return ErrPrivateTarget
	}
	return nil
}
