package server

import (
	"aitools/internal/fsguard"
	"aitools/internal/netdiag"
	"aitools/internal/sessions"
	"aitools/web"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Root struct {
	ID      string `json:"root_id"`
	Label   string `json:"label"`
	Display string `json:"path_display"`
	Source  string `json:"source"`
	Exists  bool   `json:"exists"`
	path    string
}
type App struct {
	searches             chan struct{}
	Host, Token, Version string
	roots                []Root
	items                map[string][]sessions.Session
	mu                   sync.RWMutex
	busy                 chan struct{}
	last                 atomic.Int64
	started              time.Time
	Stop                 func()
}

func New(host, version string) *App {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	a := &App{Host: host, Version: version, Token: base64.RawURLEncoding.EncodeToString(b), items: map[string][]sessions.Session{}, busy: make(chan struct{}, 1), searches: make(chan struct{}, 2), started: time.Now(), Stop: func() {}}
	p := filepath.Join(netdiag.ConfigDir(), "projects")
	a.roots = []Root{makeRoot(p, "默认 Claude 记录", "default")}
	a.last.Store(time.Now().UnixNano())
	return a
}
func makeRoot(p, label, source string) Root {
	h := sha256.Sum256([]byte(p))
	st, e := os.Stat(p)
	return Root{hex.EncodeToString(h[:16]), label, fsguard.Display(p), source, e == nil && st.IsDir(), p}
}
func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": strconv.Itoa(code), "message": msg}})
}
func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func body(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		fail(w, 400, "请求格式无效")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "请求含多余内容")
		return false
	}
	return true
}
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if r.Host != a.Host {
		fail(w, 421, "请求主机无效")
		return
	}
	origin := "http://" + a.Host
	if o := r.Header.Get("Origin"); o != "" && o != origin {
		fail(w, 403, "请求来源无效")
		return
	}
	api := strings.HasPrefix(r.URL.Path, "/api/")
	if api {
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			fail(w, 403, "仅接受本机页面请求")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-AITools-Token")), []byte(a.Token)) != 1 {
			fail(w, 401, "请从启动地址重新打开工具")
			return
		}
		if r.Method == "POST" && (r.Header.Get("Origin") != origin || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json") {
			fail(w, 403, "请求来源或格式无效")
			return
		}
		a.last.Store(time.Now().UnixNano())
	} else {
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "不支持此方法")
			return
		}
		http.FileServer(http.FS(web.Files)).ServeHTTP(w, r)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/info":
		send(w, map[string]any{"version": a.Version, "os": runtime.GOOS, "arch": runtime.GOARCH, "started_at": a.started, "rule_version": netdiag.RuleVersion, "idle_timeout_s": 1800})
	case "POST /api/shutdown":
		send(w, map[string]bool{"ok": true})
		go a.Stop()
	case "GET /api/sessions/roots":
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, a.roots)
	case "POST /api/sessions/roots":
		var req struct {
			Path string `json:"path"`
		}
		if !body(w, r, &req) {
			return
		}
		p, e := fsguard.Root(req.Path)
		if req.Path == "" || e != nil {
			fail(w, 400, "请选择存在的具体记录目录，不能选择家目录、系统目录或磁盘根目录")
			return
		}
		root := makeRoot(p, filepath.Base(p), "user")
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, x := range a.roots {
			if x.ID == root.ID {
				send(w, x)
				return
			}
		}
		if len(a.roots) >= 8 {
			fail(w, 400, "最多添加 8 个目录，请退出后重试")
			return
		}
		a.roots = append(a.roots, root)
		send(w, root)
	case "POST /api/sessions/scan":
		var req struct {
			ID string `json:"root_id"`
		}
		if !body(w, r, &req) {
			return
		}
		select {
		case a.busy <- struct{}{}:
			defer func() { <-a.busy }()
		default:
			fail(w, 409, "已有检查正在进行")
			return
		}
		a.mu.RLock()
		p := ""
		for _, root := range a.roots {
			if root.ID == req.ID {
				p = root.path
			}
		}
		a.mu.RUnlock()
		if p == "" {
			fail(w, 404, "记录目录不存在")
			return
		}
		items, sum, e := sessions.Scan(r.Context(), p)
		if e != nil {
			fail(w, 400, "目录不存在或无法读取")
			return
		}
		a.mu.Lock()
		a.items = map[string][]sessions.Session{req.ID: items}
		a.mu.Unlock()
		send(w, sum)
	case "GET /api/sessions/search":
		select {
		case a.searches <- struct{}{}:
			defer func() { <-a.searches }()
		default:
			fail(w, 429, "同时检索过多，请稍后重试")
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		a.mu.RLock()
		items := a.items[q.Get("root_id")]
		a.mu.RUnlock()
		rows, n, e := sessions.Search(items, sessions.Query{Q: q.Get("q"), Project: q.Get("project"), From: q.Get("from"), To: q.Get("to"), Limit: limit, Offset: offset})
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		projects := map[string]bool{}
		for _, s := range items {
			projects[s.Project] = true
		}
		send(w, map[string]any{"total": n, "items": rows, "projects": projects})
	case "GET /api/sessions/item", "GET /api/sessions/export":
		a.mu.RLock()
		var found *sessions.Session
		for _, items := range a.items {
			for _, s := range items {
				if s.Key == r.URL.Query().Get("session_key") {
					copy := s
					found = &copy
					break
				}
			}
		}
		a.mu.RUnlock()
		if found == nil {
			fail(w, 404, "未找到记录，请重新扫描")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/export") {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="session-%s.md"`, found.Key[:8]))
			io.WriteString(w, sessions.Markdown(*found))
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		page = min(max(page, 0), len(found.Messages)/200)
		start := min(page*200, len(found.Messages))
		end := min(start+200, len(found.Messages))
		total := len(found.Messages)
		found.Messages = found.Messages[start:end]
		send(w, map[string]any{"session": found, "total_messages": total, "page": max(page, 0)})
	case "POST /api/net/run":
		var req struct {
			Project string `json:"project_dir"`
			Probe   bool   `json:"probe"`
		}
		if !body(w, r, &req) {
			return
		}
		if req.Project != "" {
			p, e := fsguard.Root(req.Project)
			if e != nil {
				fail(w, 400, "项目目录不存在或不允许读取")
				return
			}
			req.Project = p
		}
		select {
		case a.busy <- struct{}{}:
			defer func() { <-a.busy }()
		default:
			fail(w, 409, "已有检查正在进行")
			return
		}
		send(w, netdiag.Collect(r.Context(), req.Project, req.Probe, a.Version))
	default:
		fail(w, 404, "接口不存在")
	}
}
func Serve(ctx context.Context, port int, version string, ready func(string)) error {
	return serve(ctx, port, version, 30*time.Minute, ready)
}
func serve(ctx context.Context, port int, version string, idle time.Duration, ready func(string)) error {
	ln, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		return e
	}
	app := New(ln.Addr().String(), version)
	s := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	app.Stop = cancel
	go func() {
		tick := time.NewTicker(min(time.Second, idle/4))
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				shutdownCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
				s.Shutdown(shutdownCtx)
				c()
				return
			case <-tick.C:
				if time.Since(time.Unix(0, app.last.Load())) > idle {
					cancel()
				}
			}
		}
	}()
	ready("http://" + app.Host + "/#t=" + app.Token)
	e = s.Serve(ln)
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
