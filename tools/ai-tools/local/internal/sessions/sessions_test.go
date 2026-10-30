package sessions

import (
	"aitools/internal/fsguard"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const sample = `{"type":"user","sessionId":"demo","cwd":"/demo","timestamp":"2026-10-01T10:00:00Z","message":{"content":"Hello 世界"}}
{"type":"assistant","timestamp":"2026-10-03T10:00:00Z","message":{"id":"m1","content":[{"type":"text","text":"Answer"},{"type":"thinking","thinking":"SECRET"}]}}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"继续"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"SECRET"}]}}
{"type":"api-request-blob","message":{"content":"SECRET"}}
`

func TestParse(t *testing.T) {
	s := Parse(strings.NewReader(sample), "demo/file.jsonl", time.Now())
	if s.Status != "ok" || s.UserTurns != 1 || s.AssistantTurns != 1 || len(s.Messages) != 2 || s.Messages[1].Text != "Answer\n继续" {
		t.Fatalf("%+v", s)
	}
	if strings.Contains(Markdown(s), "SECRET") {
		t.Fatal("omitted content leaked")
	}
	if s.Omitted["thinking"] != 1 {
		t.Fatal(s.Omitted)
	}
	s = Parse(strings.NewReader("broken\n"+strings.Repeat("x", MaxLine+3)+"\n"+sample+"{\"type\":\"future\"}\n"), "a/b", time.Now())
	if s.BadLines != 1 || s.Oversized != 1 || s.Unknown["future"] != 1 || s.Status != "partial" {
		t.Fatalf("%+v", s)
	}
	for _, v := range []string{"", `{"type":"future"}`} {
		if Parse(strings.NewReader(v), "a/b", time.Now()).Status != "unknown" {
			t.Fatal("unknown")
		}
	}
}
func TestSearch(t *testing.T) {
	s := Parse(strings.NewReader(sample), "demo/file", time.Now())
	for _, tc := range []struct {
		q Query
		n int
	}{{Query{Q: "hello 世界"}, 1}, {Query{Q: "hello absent"}, 0}, {Query{From: "2026-10-02", To: "2026-10-02"}, 0}, {Query{From: "2026-10-03", To: "2026-10-03"}, 1}, {Query{Project: "wrong"}, 0}} {
		_, n, e := Search([]Session{s}, tc.q)
		if e != nil || n != tc.n {
			t.Fatalf("%+v %d %v", tc.q, n, e)
		}
	}
	if _, _, e := Search(nil, Query{From: "bogus"}); e == nil {
		t.Fatal("invalid date")
	}
}
func TestScanReadOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	os.WriteFile(p, []byte(sample), 0600)
	before, _ := os.Stat(p)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte(sample), 0600)
	os.Symlink(outside, filepath.Join(dir, "escape.jsonl"))
	f, _ := os.Create(filepath.Join(dir, "large.jsonl"))
	f.Truncate(MaxFile + 1)
	f.Close()
	items, sum, e := Scan(context.Background(), dir)
	if e != nil || len(items) != 1 || sum.TooLarge != 1 || sum.Symlinks != 1 {
		t.Fatalf("%+v %v", sum, e)
	}
	after, _ := os.Stat(p)
	b, _ := os.ReadFile(p)
	if !before.ModTime().Equal(after.ModTime()) || string(b) != sample {
		t.Fatal("modified source")
	}
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "a.jsonl"), []byte(sample), 0600)
	items2, _, _ := Scan(context.Background(), other)
	if items[0].Key == items2[0].Key {
		t.Fatal("root identity collision")
	}
	for _, bad := range []string{"/", os.Getenv("HOME"), filepath.Dir(os.Getenv("HOME")), "/System"} {
		if _, e := fsguard.Root(bad); e == nil {
			t.Fatal("accepted root", bad)
		}
	}
}

func TestCancelledParse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := parse(ctx, strings.NewReader(sample), "a/b", time.Now())
	if s.Status != "error" || len(s.Messages) != 0 {
		t.Fatal("cancel ignored")
	}
}
func TestMarkdownGolden(t *testing.T) {
	s := Session{Title: "Demo", Project: "/demo", ID: "id", Started: "start", Ended: "end", Source: "~/fixture", Status: "ok", Messages: []Message{{Role: "user", Text: "literal <tag>", Timestamp: "time"}}}
	want := "# Demo\n\n- 项目：/demo\n- 会话：id\n- 时间：start — end\n- 来源：~/fixture\n- 格式：ok（坏行 0，超长行 0）\n\n## 用户 · time\n\n```text\nliteral <tag>\n```\n\n"
	if Markdown(s) != want {
		t.Fatalf("%q", Markdown(s))
	}
}

func TestOversizeAllocationBound(t *testing.T) {
	input := strings.Repeat("x", 20<<20) + "\n" + sample
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s := Parse(strings.NewReader(input), "a/b", time.Now())
	runtime.ReadMemStats(&after)
	if s.Oversized != 1 || len(s.Messages) != 2 {
		t.Fatal("did not recover")
	}
	if after.TotalAlloc-before.TotalAlloc > 16<<20 {
		t.Fatalf("overlong line allocated %d bytes", after.TotalAlloc-before.TotalAlloc)
	}
}
func TestUnavailableAndSymlinkDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions fixture")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "good.jsonl"), []byte(sample), 0600)
	bad := filepath.Join(dir, "denied.jsonl")
	os.WriteFile(bad, []byte(sample), 0000)
	defer os.Chmod(bad, 0600)
	if f, e := os.Open(bad); e == nil {
		f.Close()
		t.Skip("current user bypasses mode permissions")
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "outside.jsonl"), []byte(sample), 0600)
	os.Symlink(outside, filepath.Join(dir, "linked"))
	items, sum, e := Scan(context.Background(), dir)
	if e != nil || len(items) != 2 || sum.Symlinks != 1 {
		t.Fatalf("%+v %v", sum, e)
	}
	errors := 0
	for _, s := range items {
		if s.Status == "error" {
			errors++
		}
	}
	if errors != 1 {
		t.Fatal("missing unreadable file result")
	}
}

func TestMarkdownCannotForgeRole(t *testing.T) {
	s := Session{Title: "hello\n## Fake", Messages: []Message{{Role: "user", Text: "```\n## Claude · spoof\n<script>"}}}
	out := Markdown(s)
	if !strings.Contains(out, "````text\n```\n## Claude · spoof") || strings.HasPrefix(out, "# hello\n") {
		t.Fatal(out)
	}
}
func TestSearchDoesNotCopyCorpus(t *testing.T) {
	text := strings.Repeat("a", 1<<20) + "MATCH"
	s := Session{Messages: []Message{{Text: text, lower: strings.ToLower(text)}}}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	rows, n, e := Search([]Session{s}, Query{Q: "match"})
	runtime.ReadMemStats(&after)
	if e != nil || n != 1 || !strings.Contains(rows[0].Snippet, "MATCH") {
		t.Fatal(e, rows)
	}
	if after.TotalAlloc-before.TotalAlloc > 256<<10 {
		t.Fatal("search copied corpus")
	}
}
