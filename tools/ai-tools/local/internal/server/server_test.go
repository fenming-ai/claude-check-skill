package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func request(a *App, method, path, body, token, origin, host, site string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+a.Host+path, strings.NewReader(body))
	r.Host = host
	if token != "" {
		r.Header.Set("X-AITools-Token", token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func TestBoundary(t *testing.T) {
	a := New("127.0.0.1:4567", "test")
	for _, tc := range []struct {
		token, origin, host, site string
		want                      int
	}{{"", "", a.Host, "", 401}, {"wrong", "", a.Host, "", 401}, {a.Token, "http://evil.test", a.Host, "", 403}, {a.Token, "", "evil.test", "", 421}, {a.Token, "", a.Host, "cross-site", 403}, {a.Token, "", a.Host, "same-origin", 200}} {
		w := request(a, "GET", "/api/info", "", tc.token, tc.origin, tc.host, tc.site)
		if w.Code != tc.want {
			t.Fatalf("%+v got %d", tc, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("headers")
		}
	}
	if w := request(a, "POST", "/api/net/run", "{}", a.Token, "", a.Host, ""); w.Code != 403 {
		t.Fatal("POST origin")
	}
	if w := request(a, "GET", "/", "", "", "", a.Host, "none"); w.Code != 200 {
		t.Fatal("initial navigation blocked")
	}
	if w := request(a, "GET", "/api/sessions/item?session_key=../../etc/passwd", "", a.Token, "", a.Host, ""); w.Code != 404 {
		t.Fatal("path key accepted")
	}
}
func TestWorkflow(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(`{"type":"user","message":{"content":"<script>bad()</script>"}}`), 0600)
	a := New("127.0.0.1:4567", "test")
	post := func(path string, v any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(v)
		return request(a, "POST", path, string(b), a.Token, "http://"+a.Host, a.Host, "same-origin")
	}
	w := post("/api/sessions/roots", map[string]string{"path": dir})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var root Root
	json.Unmarshal(w.Body.Bytes(), &root)
	w = post("/api/sessions/scan", map[string]string{"root_id": root.ID})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/sessions/search?root_id="+root.ID, "", a.Token, "", a.Host, "")
	var result struct {
		Items []struct {
			Key string `json:"session_key"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Items) != 1 {
		t.Fatal(w.Body.String())
	}
	pageCheck := request(a, "GET", "/api/sessions/item?session_key="+result.Items[0].Key+"&page=9223372036854775807", "", a.Token, "", a.Host, "")
	if pageCheck.Code != 200 {
		t.Fatal("huge page panicked")
	}
	w = request(a, "GET", "/api/sessions/export?session_key="+result.Items[0].Key, "", a.Token, "", a.Host, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<script>bad()</script>") || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatal("export")
	}
	if w = post("/api/sessions/roots", map[string]string{"path": "/"}); w.Code != 400 {
		t.Fatal("root allowed")
	}
	if w = post("/api/net/run", map[string]any{"probe": false, "unexpected": "x"}); w.Code != 400 {
		t.Fatal("extra field")
	}
}

func TestIdleShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if e := serve(ctx, 0, "test", 80*time.Millisecond, func(string) {}); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("idle shutdown did not run")
	}
}
