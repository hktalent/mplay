package netguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":            true,
		"127.1.2.3":            true,
		"10.1.2.3":             true,
		"172.16.0.1":           true,
		"192.168.1.1":          true,
		"169.254.169.254":      true, // 云主机元数据地址
		"100.64.0.1":           true, // CGNAT
		"0.0.0.0":              true,
		"::1":                  true,
		"fd00::1":              true,
		"fe80::1":              true,
		"8.8.8.8":              false,
		"1.1.1.1":              false,
		"2001:4860:4860::8888": false,
	}
	for s, want := range cases {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("测试用例写错了: %s", s)
		}
		if got := IsPrivateIP(ip); got != want {
			t.Errorf("IsPrivateIP(%s) = %v, 期望 %v", s, got, want)
		}
	}
}

func TestCheckURLBlocksPrivateAndBadSchemes(t *testing.T) {
	bad := []string{
		"http://127.0.0.1:8831/api/config",
		"http://localhost:8080/x",
		"http://[::1]:80/",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.168.31.1/",
		"file:///etc/passwd",
		"gopher://127.0.0.1:6379/_INFO",
		"ftp://example.com/x",
		"",
	}
	for _, u := range bad {
		if err := CheckURL(nil, u); err == nil {
			t.Errorf("CheckURL(%q) 应该被拒绝", u)
		}
	}
	if err := CheckURL(nil, "https://example.com/search?q=x"); err != nil {
		t.Errorf("公网地址应放行，实际 %v", err)
	}
}

func TestIsLocalRequest(t *testing.T) {
	mk := func(remote string, hdr map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	if !IsLocalRequest(mk("127.0.0.1:5555", nil)) {
		t.Error("回环直连应判为本机")
	}
	// 反向代理：RemoteAddr 被改写成回环，但带了转发头 → 必须不认作本机
	if IsLocalRequest(mk("127.0.0.1:5555", map[string]string{"X-Forwarded-For": "203.0.113.9"})) {
		t.Error("带 X-Forwarded-For 的请求不应判为本机")
	}
	// 伪造 X-Forwarded-For: 127.0.0.1 也不放行：只看有没有代理头
	if IsLocalRequest(mk("127.0.0.1:5555", map[string]string{"X-Forwarded-For": "127.0.0.1"})) {
		t.Error("伪造的转发头不应绕过本机判定")
	}
	if IsLocalRequest(mk("203.0.113.9:5555", nil)) {
		t.Error("公网对端不应判为本机")
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8737/api/config", nil)
	r.Host = "127.0.0.1:8737"
	if !SameOrigin(r) {
		t.Error("无 Origin 的写请求应放行（curl/桌面壳）")
	}
	r.Header.Set("Origin", "https://evil.example")
	if SameOrigin(r) {
		t.Error("外站 Origin 应被拒")
	}
	r.Header.Set("Origin", "http://127.0.0.1:8737")
	if !SameOrigin(r) {
		t.Error("同源 Origin 应放行")
	}
}
