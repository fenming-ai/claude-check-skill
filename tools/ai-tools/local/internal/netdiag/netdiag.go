package netdiag

import (
	"aitools/internal/fsguard"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const RuleVersion = "2026-10-27"

var envNames = []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "NODE_EXTRA_CA_CERTS", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

type Field struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
}
type Evidence struct {
	Detail string  `json:"detail,omitempty"`
	Scope  string  `json:"scope"`
	Path   string  `json:"path_display"`
	Status string  `json:"status"`
	Fields []Field `json:"fields"`
}
type Stage struct {
	Code   string `json:"code,omitempty"`
	Name   string `json:"name"`
	Status string `json:"status"`
	MS     int64  `json:"ms"`
	Detail string `json:"detail,omitempty"`
}
type Probe struct {
	customCA bool
	ProxyVar string  `json:"proxy_var,omitempty"`
	ID       string  `json:"id"`
	Target   string  `json:"target"`
	Route    string  `json:"route"`
	Proxy    string  `json:"proxy,omitempty"`
	Source   string  `json:"proxy_source,omitempty"`
	Stages   []Stage `json:"stages"`
	HTTP     int     `json:"http_status,omitempty"`
	IP       string  `json:"observed_ip,omitempty"`
	Result   string  `json:"result"`
	Started  string  `json:"started_at"`
	Note     string  `json:"note,omitempty"`
}
type Report struct {
	Schema      string            `json:"schema"`
	RuleVersion string            `json:"rule_version"`
	Generated   string            `json:"generated_at"`
	Tool        map[string]string `json:"tool"`
	Environment struct {
		Note string  `json:"note"`
		Vars []Field `json:"vars"`
	} `json:"detector_env"`
	Configs    []Evidence        `json:"config_evidence"`
	Duplicates []map[string]any  `json:"same_key_in_scopes"`
	Probes     []Probe           `json:"probes"`
	Running    map[string]string `json:"running_claude"`
	Notes      []string          `json:"notes"`
}
type route struct{ raw, source, variable string }
type target struct{ id, url string }

var targets = []target{{"echo-v4", "https://api.ipify.org?format=json"}, {"echo-v6", "https://api6.ipify.org?format=json"}, {"reach-api", "https://api.anthropic.com/"}, {"reach-web", "https://claude.ai/"}}

func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Clean(d)
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}
func managedPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	default:
		return "/etc/claude-code/managed-settings.json"
	}
}
func safeValue(name, v string) string {
	n := strings.ToUpper(name)
	if strings.Contains(n, "KEY") || strings.Contains(n, "TOKEN") || name == "apiKeyHelper" {
		return ""
	}
	if n == "NO_PROXY" {
		return "[已设置，规则不导出]"
	}
	if n == "NODE_EXTRA_CA_CERTS" {
		return fsguard.Display(v)
	}
	if strings.Contains(n, "PROXY") || n == "ANTHROPIC_BASE_URL" {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || u.Scheme == "" {
			return "[格式无法识别]"
		}
		u.User = nil
		u.Path = ""
		u.RawPath = ""
		u.RawQuery = ""
		u.Fragment = ""
		u.ForceQuery = false
		return u.String()
	}
	if v == "true" || v == "false" || v == "0" || v == "1" {
		return v
	}
	return "[已设置]"
}
func Collect(ctx context.Context, project string, online bool, version string) Report {
	r := Report{Schema: "aitools.netreport/0.1", RuleVersion: RuleVersion, Generated: time.Now().UTC().Format(time.RFC3339), Tool: map[string]string{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH}, Configs: []Evidence{}, Duplicates: []map[string]any{}, Probes: []Probe{}, Running: map[string]string{"status": "not_checked", "note": "未检测正在运行的 Claude 进程；测试请求的出口不代表它的出口。"}, Notes: []string{"仅采集固定配置文件，未覆盖 MDM、注册表、服务端策略及 managed-settings.d。", "重复配置仅列证据，不推断实际生效顺序。", "探测不携带登录凭证；不使用配置中的自定义 CA。代理测试不应用 NO_PROXY 绕行规则。"}}
	r.Environment.Note = "这是检测器进程的环境，双击启动可能与终端不同；要采集终端环境，请在该终端运行 aitools net。"
	r.Environment.Vars = []Field{}
	routes := []route{{"", "direct", ""}}
	seen := map[string]bool{}
	hasCustomCA := false
	add := func(name, v, source string) {
		if !strings.HasSuffix(strings.ToUpper(name), "_PROXY") || strings.EqualFold(name, "NO_PROXY") || v == "" || seen[v] {
			return
		}
		seen[v] = true
		if len(routes) < 9 {
			routes = append(routes, route{v, source, name})
		} else {
			r.Notes = append(r.Notes, "代理数量超过 8 个，仅测试前 8 个。")
		}
	}
	for _, name := range envNames {
		for _, key := range []string{name, strings.ToLower(name)} {
			v, ok := os.LookupEnv(key)
			if ok {
				if strings.EqualFold(key, "NODE_EXTRA_CA_CERTS") && v != "" {
					hasCustomCA = true
				}
				r.Environment.Vars = append(r.Environment.Vars, Field{key, true, safeValue(key, v)})
				add(key, v, "detector_env")
			}
		}
	}
	files := []struct{ scope, path string }{{"user", filepath.Join(ConfigDir(), "settings.json")}, {"managed", managedPath()}}
	if project != "" {
		files = append(files, struct{ scope, path string }{"project", filepath.Join(project, ".claude", "settings.json")}, struct{ scope, path string }{"project_local", filepath.Join(project, ".claude", "settings.local.json")})
	}
	duplicate := map[string][]string{}
	for _, file := range files {
		var b []byte
		var status string
		switch file.scope {
		case "user":
			dir := ConfigDir()
			b, status = fsguard.ReadUnder(filepath.Dir(dir), filepath.Join(filepath.Base(dir), "settings.json"), 1<<20)
		case "project", "project_local":
			rel, _ := filepath.Rel(project, file.path)
			b, status = fsguard.ReadUnder(project, rel, 1<<20)
		default:
			b, status = fsguard.Read(file.path, 1<<20)
		}
		e := Evidence{Scope: file.scope, Path: fsguard.Display(file.path), Status: status, Fields: []Field{}}
		if status == "skipped_symlink" {
			e.Detail = "Claude Code 可能经此链接读取配置，本工具未读取。"
		}
		if status == "found" {
			var obj map[string]json.RawMessage
			if json.Unmarshal(b, &obj) != nil || obj == nil {
				e.Status = "parse_error"
			} else {
				var env map[string]json.RawMessage
				if raw, ok := obj["env"]; ok && json.Unmarshal(raw, &env) != nil {
					e.Status = "parse_error"
				}
				for _, name := range envNames {
					for _, key := range []string{name, strings.ToLower(name)} {
						if raw, ok := env[key]; ok {
							var v string
							if json.Unmarshal(raw, &v) != nil {
								e.Fields = append(e.Fields, Field{"env." + key, true, "[值类型异常]"})
								continue
							}
							if strings.EqualFold(key, "NODE_EXTRA_CA_CERTS") && v != "" {
								hasCustomCA = true
							}
							e.Fields = append(e.Fields, Field{"env." + key, true, safeValue(key, v)})
							duplicate["env."+key] = append(duplicate["env."+key], file.scope)
							add(key, v, file.scope)
						}
					}
				}
				if _, ok := obj["apiKeyHelper"]; ok {
					e.Fields = append(e.Fields, Field{"apiKeyHelper", true, ""})
				}
			}
		}
		r.Configs = append(r.Configs, e)
	}
	for k, v := range duplicate {
		if len(v) > 1 {
			r.Duplicates = append(r.Duplicates, map[string]any{"key": k, "scopes": v})
		}
	}
	sort.Slice(r.Duplicates, func(i, j int) bool { return r.Duplicates[i]["key"].(string) < r.Duplicates[j]["key"].(string) })
	r.Notes = append(r.Notes, "代理是逐个独立测试，proxy_var 记录首次发现的来源变量；HTTP_PROXY 测试结果不表示 HTTPS 请求实际采用它。")
	for _, rt := range routes {
		for _, t := range targets {
			p := Probe{customCA: hasCustomCA, ID: t.id, Target: t.url, Route: "direct", Stages: []Stage{}, Result: "not_checked"}
			if rt.raw != "" {
				p.Route = "proxy"
				p.Proxy = safeValue("HTTPS_PROXY", rt.raw)
				p.Source = rt.source
				p.ProxyVar = rt.variable
			}
			r.Probes = append(r.Probes, p)
		}
	}
	if !online {
		r.Notes = append(r.Notes, "离线采集，没有发出测试请求。")
		return r
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range r.Probes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			r.Probes[i] = probe(ctx, r.Probes[i], routes[i/len(targets)].raw, nil)
		}(i)
	}
	wg.Wait()
	return r
}
func probe(ctx context.Context, p Probe, proxy string, tlsConfig *tls.Config) Probe {
	p.Started = time.Now().UTC().Format(time.RFC3339)
	p.Result = "problem"
	var proxyURL *url.URL
	if proxy != "" {
		u, e := url.Parse(proxy)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			p.Result = "not_checked"
			p.Stages = []Stage{{Name: "proxy", Status: "unsupported", Detail: "只支持不含认证、路径或参数的 HTTP/HTTPS 代理；未回退直连。"}}
			return p
		}
		proxyURL = u
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, networkFor(p.ID, proxyURL != nil), address)
	}, TLSHandshakeTimeout: 5 * time.Second, TLSClientConfig: tlsConfig, DisableKeepAlives: true}
	if proxyURL != nil {
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var mu sync.Mutex
	starts := map[string]time.Time{}
	stages := []Stage{}
	begin := func(n string) { mu.Lock(); starts[n] = time.Now(); mu.Unlock() }
	finish := func(n string, e error) {
		mu.Lock()
		defer mu.Unlock()
		status := "ok"
		detail := ""
		if e != nil {
			status = "fail"
			detail = "此阶段未完成，请核对网络或代理配置。"
			if errorCode(e) == "tls_unknown_authority" {
				detail = "证书颁发机构不受信任。"
				if p.customCA {
					detail += "已发现 NODE_EXTRA_CA_CERTS 配置；检测器未加载该自定义 CA。"
				}
			}
		}
		ms := int64(0)
		if t, ok := starts[n]; ok {
			ms = time.Since(t).Milliseconds()
		}
		stages = append(stages, Stage{Name: n, Status: status, MS: ms, Detail: detail, Code: errorCode(e)})
	}
	proxyStatus := 0
	tr.OnProxyConnectResponse = func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
		mu.Lock()
		defer mu.Unlock()
		proxyStatus = response.StatusCode
		status := "ok"
		if response.StatusCode != 200 {
			status = "fail"
		}
		stages = append(stages, Stage{Name: "proxy_tunnel", Status: status, Code: proxyCode(response.StatusCode), Detail: fmt.Sprintf("代理 CONNECT 响应 HTTP %d", response.StatusCode)})
		return nil
	}
	dnsStage, connectStage := "dns", "connect"
	if proxyURL != nil {
		dnsStage = "proxy_dns"
		connectStage = "proxy_connect"
	}
	trace := &httptrace.ClientTrace{DNSStart: func(httptrace.DNSStartInfo) { begin(dnsStage) }, DNSDone: func(i httptrace.DNSDoneInfo) { finish(dnsStage, i.Err) }, ConnectStart: func(string, string) { begin(connectStage) }, ConnectDone: func(_, _ string, e error) { finish(connectStage, e) }, TLSHandshakeStart: func() { begin("tls") }, TLSHandshakeDone: func(_ tls.ConnectionState, e error) { finish("tls", e) }}
	req, e := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", p.Target, nil)
	if e != nil {
		p.Note = "请求地址无效"
		return p
	}
	req.Header.Set("User-Agent", "aitools/0.1")
	begin("http")
	resp, e := client.Do(req)
	finish("http", e)
	mu.Lock()
	p.Stages = append(p.Stages, stages...)
	mu.Unlock()
	if proxyURL != nil {
		p.Stages = append(p.Stages, Stage{Name: "target_dns", Status: "skipped", Detail: "目标由代理解析；DNS 事件只反映代理连接。"})
	}
	if e != nil {
		p.Note = "未取得目标 HTTP 响应；未使用其他路径重试。"
		if proxyStatus != 0 && proxyStatus != 200 {
			p.Note = fmt.Sprintf("代理拒绝 CONNECT，HTTP %d；请求未到达目标 HTTP 服务。", proxyStatus)
		}
		return p
	}
	defer resp.Body.Close()
	p.HTTP = resp.StatusCode
	p.Result = "verified"
	p.Note = "服务已响应，不代表登录有效或账号状态。"
	if strings.HasPrefix(p.ID, "echo-") {
		b, e := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		var data struct {
			IP string `json:"ip"`
		}
		if e != nil || len(b) > 64<<10 || resp.StatusCode != 200 || json.Unmarshal(b, &data) != nil || net.ParseIP(data.IP) == nil {
			p.Result = "undetermined"
			p.Note = "服务有响应，但未取得有效 IP。"
		} else {
			p.IP = data.IP
		}
		if p.ID == "echo-v6" && proxyURL != nil {
			p.Result = "undetermined"
			p.Note = "该结果仅反映代理出口，不能证明本机 IPv6 可用。"
		}
	}
	return p
}

func networkFor(id string, proxy bool) string {
	if proxy {
		return "tcp"
	}
	switch id {
	case "echo-v4":
		return "tcp4"
	case "echo-v6":
		return "tcp6"
	}
	return "tcp"
}
func proxyCode(status int) string {
	if status == 407 {
		return "proxy_auth_required"
	}
	if status != 200 {
		return "proxy_refused"
	}
	return ""
}
func errorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "tls_unknown_authority"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		return "dns_not_found"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return "tls_certificate_invalid"
	}
	return "connection_failed"
}
