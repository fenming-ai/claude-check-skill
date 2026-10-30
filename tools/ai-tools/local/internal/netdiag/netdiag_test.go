package netdiag

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflinePrivacy(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_API_KEY", "VERY_SECRET")
	t.Setenv("HTTPS_PROXY", "http://user:PASSWORD@127.0.0.1:1234/path?secret=QUERY")
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"TOKEN_SECRET","HTTPS_PROXY":"http://u:PASS@localhost:1234?key=HIDDEN"},"apiKeyHelper":"SECRET_HELPER"}`), 0600)
	r := Collect(context.Background(), "", false, "test")
	b, _ := json.Marshal(r)
	for _, bad := range []string{"VERY_SECRET", "PASSWORD", "QUERY", "TOKEN_SECRET", "PASS@", "HIDDEN", "SECRET_HELPER"} {
		if strings.Contains(string(b), bad) {
			t.Fatal("leak", bad)
		}
	}
	for _, p := range r.Probes {
		if p.Result != "not_checked" || p.Started != "" {
			t.Fatal("offline probe")
		}
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("broken"), 0600)
	if Collect(context.Background(), "", false, "test").Configs[0].Status != "parse_error" {
		t.Fatal("parse")
	}
}
func TestProbe(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials")
		}
		switch r.URL.Path {
		case "/deny":
			w.WriteHeader(403)
		case "/big":
			io.WriteString(w, strings.Repeat("x", 65537))
		default:
			io.WriteString(w, `{"ip":"203.0.113.1"}`)
		}
	}))
	defer s.Close()
	cfg := s.Client().Transport.(*http.Transport).TLSClientConfig
	for _, tc := range []struct{ path, id, want string }{{"/", "echo-v4", "verified"}, {"/deny", "reach-api", "verified"}, {"/big", "echo-v4", "undetermined"}} {
		p := probe(context.Background(), Probe{ID: tc.id, Target: s.URL + tc.path}, "", cfg)
		if p.Result != tc.want {
			t.Fatalf("%+v", p)
		}
	}
	if p := probe(context.Background(), Probe{Target: s.URL}, "", nil); p.Result != "problem" {
		t.Fatal("TLS trusted unexpectedly")
	}
	for _, v := range []string{"socks5://localhost:1234", "http://u:p@localhost:1234"} {
		if p := probe(context.Background(), Probe{Target: s.URL}, v, cfg); p.Result != "not_checked" {
			t.Fatal(p)
		}
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if p := probe(context.Background(), Probe{Target: s.URL}, "http://"+addr, cfg); p.Result != "problem" {
		t.Fatal("proxy fallback")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p := probe(ctx, Probe{Target: s.URL}, "", cfg); p.Result != "problem" {
		t.Fatal("cancel")
	}
}
func TestConnectProxy(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"ip":"2001:db8::1"}`) }))
	defer target.Close()
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "CONNECT" {
			t.Error("expected CONNECT")
			w.WriteHeader(400)
			return
		}
		up, e := net.Dial("tcp", r.Host)
		if e != nil {
			w.WriteHeader(502)
			return
		}
		client, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			up.Close()
			return
		}
		io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { defer up.Close(); io.Copy(up, client) }()
		go func() { defer client.Close(); io.Copy(client, up) }()
	}))
	defer proxy.Close()
	p := probe(context.Background(), Probe{ID: "echo-v6", Target: target.URL}, proxy.URL, &tls.Config{RootCAs: target.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs})
	if calls != 1 || p.Result != "undetermined" || p.IP == "" {
		t.Fatalf("%+v calls=%d", p, calls)
	}
}

func TestProxyReject(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer s.Close()
	p := probe(context.Background(), Probe{Target: "https://example.invalid/"}, s.URL, nil)
	if p.Result != "problem" || !strings.Contains(p.Note, "代理拒绝 CONNECT，HTTP 403") {
		t.Fatalf("%+v", p)
	}
	found := false
	for _, stage := range p.Stages {
		if stage.Name == "proxy_tunnel" && stage.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing proxy evidence")
	}
}

func TestConfigScopes(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	user := filepath.Join(dir, "config")
	project := filepath.Join(dir, "project")
	os.MkdirAll(user, 0700)
	os.MkdirAll(filepath.Join(project, ".claude"), 0700)
	t.Setenv("CLAUDE_CONFIG_DIR", user)
	payload := []byte(`{"env":{"HTTPS_PROXY":"http://localhost:1234"}}`)
	os.WriteFile(filepath.Join(user, "settings.json"), payload, 0600)
	os.WriteFile(filepath.Join(project, ".claude/settings.json"), payload, 0600)
	r := Collect(context.Background(), project, false, "test")
	if len(r.Duplicates) != 1 {
		t.Fatal(r.Duplicates)
	}
	os.WriteFile(filepath.Join(user, "settings.json"), []byte(strings.Repeat("x", (1<<20)+1)), 0600)
	if Collect(context.Background(), project, false, "test").Configs[0].Status != "too_large" {
		t.Fatal("unbounded config")
	}
}

func TestProxyCodesAndNetwork(t *testing.T) {
	for _, status := range []int{407, 502} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		p := probe(context.Background(), Probe{Target: "https://example.invalid/"}, s.URL, nil)
		s.Close()
		found := false
		for _, stage := range p.Stages {
			if stage.Name == "proxy_tunnel" && stage.Code == proxyCode(status) {
				found = true
			}
		}
		if !found {
			t.Fatal(p)
		}
	}
	if networkFor("echo-v4", false) != "tcp4" || networkFor("echo-v6", false) != "tcp6" || networkFor("echo-v6", true) != "tcp" {
		t.Fatal("network")
	}
	if errorCode(&net.DNSError{IsNotFound: true}) != "dns_not_found" {
		t.Fatal("DNS error")
	}
}

func TestHomeAliasAndConfigSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.MkdirAll(filepath.Join(real, ".claude"), 0700)
	os.WriteFile(filepath.Join(real, ".claude/settings.json"), []byte(`{"env":{"HTTPS_PROXY":"http://localhost:9"}}`), 0600)
	alias := filepath.Join(dir, "home-alias")
	if e := os.Symlink(real, alias); e != nil {
		t.Skip(e)
	}
	t.Setenv("HOME", alias)
	t.Setenv("USERPROFILE", alias)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	r := Collect(context.Background(), "", false, "test")
	if r.Configs[0].Status != "found" {
		t.Fatal(r.Configs[0])
	}
	linked := filepath.Join(dir, "config-link")
	os.Symlink(filepath.Join(real, ".claude"), linked)
	t.Setenv("CLAUDE_CONFIG_DIR", linked)
	r = Collect(context.Background(), "", false, "test")
	if r.Configs[0].Status != "skipped_symlink" {
		t.Fatal("configuration link was followed")
	}
}

func TestCertificateHintDependsOnConfig(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer s.Close()
	for _, configured := range []bool{false, true} {
		p := probe(context.Background(), Probe{Target: s.URL, customCA: configured}, "", nil)
		found := false
		for _, stage := range p.Stages {
			if stage.Code == "tls_unknown_authority" {
				found = true
				if strings.Contains(stage.Detail, "NODE_EXTRA_CA_CERTS") != configured {
					t.Fatal(stage.Detail)
				}
			}
		}
		if !found {
			t.Fatal("missing TLS classification")
		}
	}
}

func TestConfigDirTrailingSeparator(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"NODE_EXTRA_CA_CERTS":"/fixture/ca.pem"}}`), 0600)
	t.Setenv("CLAUDE_CONFIG_DIR", dir+string(filepath.Separator))
	if ConfigDir() != filepath.Clean(dir) {
		t.Fatal("path not normalized")
	}
	r := Collect(context.Background(), "", false, "test")
	if r.Configs[0].Status != "found" || len(r.Configs[0].Fields) != 1 {
		t.Fatal(r.Configs[0])
	}
	for _, p := range r.Probes {
		if !p.customCA {
			t.Fatal("custom CA evidence lost")
		}
	}
}
