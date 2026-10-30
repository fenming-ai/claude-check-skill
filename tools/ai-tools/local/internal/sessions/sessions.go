package sessions

import (
	"aitools/internal/fsguard"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxFile = 50 << 20
const MaxLine = 1 << 20
const MaxTotal = 128 << 20

type Message struct {
	lower     string
	Role      string `json:"role"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Sidechain bool   `json:"sidechain"`
}
type Session struct {
	Key            string         `json:"session_key"`
	ID             string         `json:"session_id"`
	Project        string         `json:"project"`
	ProjectSource  string         `json:"project_source"`
	Title          string         `json:"title"`
	Source         string         `json:"source"`
	Started        string         `json:"started_at"`
	Ended          string         `json:"ended_at"`
	TimeSource     string         `json:"time_source"`
	UserTurns      int            `json:"user_turns"`
	AssistantTurns int            `json:"assistant_turns"`
	Status         string         `json:"format_status"`
	Error          string         `json:"error,omitempty"`
	Snippet        string         `json:"snippet"`
	BadLines       int            `json:"bad_lines"`
	BadLineNumbers []int          `json:"bad_line_numbers"`
	Oversized      int            `json:"oversized_lines"`
	Omitted        map[string]int `json:"omitted"`
	Unknown        map[string]int `json:"unknown_types"`
	Messages       []Message      `json:"messages,omitempty"`
	Times          []time.Time    `json:"-"`
}
type Summary struct {
	Files     int            `json:"files_seen"`
	Parsed    int            `json:"files_parsed"`
	Sessions  int            `json:"sessions"`
	Symlinks  int            `json:"skipped_symlink"`
	TooLarge  int            `json:"skipped_too_large"`
	BadLines  int            `json:"bad_lines"`
	Oversized int            `json:"oversized_lines"`
	Unknown   map[string]int `json:"unknown_types"`
	Partial   bool           `json:"partial"`
	Duration  int64          `json:"duration_ms"`
	Notes     []string       `json:"notes"`
}

var known = map[string]bool{"attachment": true, "summary": true, "ai-title": true, "last-prompt": true, "mode": true, "permission-mode": true, "system": true, "file-history-snapshot": true, "api-request": true, "api-request-blob": true, "progress": true, "queue-operation": true}

func cut(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
func excerpt(s string, start, n int) string {
	begin := -1
	count := 0
	for i := range s {
		if count == start {
			begin = i
		}
		if count == start+n && begin >= 0 {
			return s[begin:i]
		}
		count++
	}
	if begin >= 0 {
		return s[begin:]
	}
	return ""
}
func Parse(r io.Reader, source string, modified time.Time) Session {
	return parse(context.Background(), r, source, modified)
}
func parse(ctx context.Context, r io.Reader, source string, modified time.Time) Session {
	s := Session{Source: source, Status: "ok", Omitted: map[string]int{}, Unknown: map[string]int{}, Project: filepath.Base(filepath.Dir(source)), ProjectSource: "目录名推断", TimeSource: "记录时间", Messages: []Message{}}
	br := bufio.NewReaderSize(r, 64<<10)
	ids := map[string]int{}
	seenTimes := map[int64]bool{}
	var firstTime, lastTime time.Time
	lineNo := 0
	recognized := 0
	for {
		if ctx.Err() != nil {
			s.Error = "扫描已取消或超时"
			break
		}
		var line []byte
		oversized := false
		var end error
		for {
			if ctx.Err() != nil {
				end = ctx.Err()
				break
			}
			part, err := br.ReadSlice('\n')
			if len(line)+len(part) > MaxLine {
				oversized = true
				line = nil
			}
			if !oversized {
				line = append(line, part...)
			}
			if err != bufio.ErrBufferFull {
				end = err
				break
			}
		}
		if len(line) == 0 && !oversized && end == io.EOF {
			break
		}
		lineNo++
		if oversized {
			s.Oversized++
			if end != nil {
				break
			}
			continue
		}
		var rec struct {
			Type      string `json:"type"`
			ID        string `json:"sessionId"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
			Sidechain bool   `json:"isSidechain"`
			Title     string `json:"aiTitle"`
			Message   *struct {
				ID      string          `json:"id"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			s.BadLines++
			if len(s.BadLineNumbers) < 5 {
				s.BadLineNumbers = append(s.BadLineNumbers, lineNo)
			}
		} else {
			if rec.ID != "" {
				s.ID = rec.ID
			}
			if rec.Cwd != "" {
				s.Project = rec.Cwd
				s.ProjectSource = "记录 cwd"
			}
			if t, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
				if firstTime.IsZero() || t.Before(firstTime) {
					firstTime = t
				}
				if lastTime.IsZero() || t.After(lastTime) {
					lastTime = t
				}
				if !seenTimes[t.Unix()] {
					s.Times = append(s.Times, t)
					seenTimes[t.Unix()] = true
				}
			}
			if rec.Type == "ai-title" && rec.Title != "" {
				s.Title = rec.Title
			}
			if (rec.Type == "user" || rec.Type == "assistant") && rec.Message != nil {
				recognized++
				var text string
				if json.Unmarshal(rec.Message.Content, &text) != nil {
					var blocks []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					}
					if json.Unmarshal(rec.Message.Content, &blocks) == nil {
						var texts []string
						for _, b := range blocks {
							if b.Type == "text" {
								texts = append(texts, b.Text)
							} else {
								s.Omitted[b.Type]++
							}
						}
						text = strings.Join(texts, "\n")
					} else {
						s.Unknown["message.content"]++
					}
				}
				if strings.TrimSpace(text) != "" {
					if rec.Type == "assistant" && rec.Message.ID != "" {
						if idx, ok := ids[rec.Message.ID]; ok {
							s.Messages[idx].Text += "\n" + text
							goto done
						}
						ids[rec.Message.ID] = len(s.Messages)
					}
					s.Messages = append(s.Messages, Message{Role: rec.Type, Text: text, Timestamp: rec.Timestamp, Sidechain: rec.Sidechain})
					if rec.Type == "user" {
						s.UserTurns++
					} else {
						s.AssistantTurns++
					}
				}
			} else if known[rec.Type] {
				s.Omitted[rec.Type]++
			} else {
				s.Unknown[rec.Type]++
			}
		}
	done:
		if end != nil {
			if end != io.EOF {
				s.Error = "读取中断"
			}
			break
		}
	}
	for i := range s.Messages {
		s.Messages[i].lower = strings.ToLower(s.Messages[i].Text)
	}
	if s.BadLineNumbers == nil {
		s.BadLineNumbers = []int{}
	}
	if s.Title == "" {
		for _, m := range s.Messages {
			if m.Role == "user" {
				s.Title = cut(m.Text, 40)
				break
			}
		}
	}
	if s.Title == "" {
		s.Title = "无可识别对话"
	}
	if len(s.Times) == 0 {
		s.Times = []time.Time{modified}
		s.TimeSource = "文件时间"
	}
	sort.Slice(s.Times, func(i, j int) bool { return s.Times[i].Before(s.Times[j]) })
	if firstTime.IsZero() {
		firstTime = modified
		lastTime = modified
	}
	s.Started = firstTime.Format(time.RFC3339)
	s.Ended = lastTime.Format(time.RFC3339)
	if s.BadLines+s.Oversized+len(s.Unknown) > 0 {
		s.Status = "partial"
	}
	if recognized == 0 {
		s.Status = "unknown"
	}
	if s.Error != "" {
		s.Status = "error"
	}
	return s
}
func Scan(ctx context.Context, path string) ([]Session, Summary, error) {
	started := time.Now()
	sum := Summary{Unknown: map[string]int{}, Notes: []string{}}
	resolved, err := fsguard.Root(path)
	if err != nil {
		return nil, sum, err
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, sum, err
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	items := []Session{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, e error) error {
		if ctx.Err() != nil || sum.Files >= 5000 || total >= MaxTotal {
			sum.Partial = true
			return fs.SkipAll
		}
		if e != nil {
			sum.Partial = true
			return nil
		}
		if d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			sum.Symlinks++
			return nil
		}
		if d.IsDir() {
			if strings.Count(rel, "/") >= 4 {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(rel), ".jsonl") {
			return nil
		}
		sum.Files++
		info, e := d.Info()
		if e != nil {
			sum.Partial = true
			return nil
		}
		if info.Size() > MaxFile {
			sum.TooLarge++
			sum.Partial = true
			return nil
		}
		if total+info.Size() > MaxTotal {
			sum.Partial = true
			return fs.SkipAll
		}
		hash := sha256.Sum256([]byte(resolved + "\x00" + rel))
		key := hex.EncodeToString(hash[:16])
		source := fsguard.Display(filepath.Join(resolved, filepath.FromSlash(rel)))
		f, e := fsguard.Open(root, filepath.FromSlash(rel), MaxFile)
		if e != nil {
			items = append(items, Session{Key: key, Source: source, Status: "error", Error: "无法读取此记录"})
			sum.Partial = true
			return nil
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			sum.Partial = true
			return nil
		}
		budget := min(int64(MaxFile), int64(MaxTotal)-total)
		reader := &io.LimitedReader{R: f, N: budget + 1}
		s := parse(ctx, reader, source, st.ModTime())
		f.Close()
		total += budget + 1 - reader.N
		if reader.N == 0 {
			s.Status = "partial"
			s.Error = "读取时达到大小上限"
			sum.Partial = true
		}
		if ctx.Err() != nil {
			sum.Partial = true
		}
		s.Key = key
		sum.Parsed++
		sum.BadLines += s.BadLines
		sum.Oversized += s.Oversized
		for k, v := range s.Unknown {
			sum.Unknown[k] += v
		}
		items = append(items, s)
		return nil
	})
	sum.Sessions = len(items)
	sum.Duration = time.Since(started).Milliseconds()
	if sum.Partial {
		sum.Notes = append(sum.Notes, "扫描未覆盖全部文件；上限为 5000 个文件、128 MiB、60 秒。")
	}
	return items, sum, err
}

type Query struct {
	Q, Project, From, To string
	Limit, Offset        int
}

func Search(items []Session, q Query) ([]Session, int, error) {
	if len(q.Q) > 1024 || len(strings.Fields(q.Q)) > 16 {
		return nil, 0, fmt.Errorf("关键词最多 16 个，总长度最多 1024 字节")
	}
	var from, to time.Time
	var err error
	if q.From != "" {
		from, err = time.Parse("2006-01-02", q.From)
		if err != nil {
			return nil, 0, fmt.Errorf("开始日期无效")
		}
	}
	if q.To != "" {
		to, err = time.Parse("2006-01-02", q.To)
		if err != nil {
			return nil, 0, fmt.Errorf("结束日期无效")
		}
		to = to.Add(24 * time.Hour)
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return nil, 0, fmt.Errorf("日期范围无效")
	}
	out := []Session{}
	words := strings.Fields(strings.ToLower(q.Q))
	for _, s := range items {
		if q.Project != "" && s.Project != q.Project {
			continue
		}
		if !from.IsZero() || !to.IsZero() {
			match := false
			for _, t := range s.Times {
				if (from.IsZero() || !t.Before(from)) && (to.IsZero() || t.Before(to)) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		matched := make([]bool, len(words))
		snippet := ""
		snippetSet := false
		for _, m := range s.Messages {
			lower := m.lower
			if lower == "" && m.Text != "" {
				lower = strings.ToLower(m.Text)
			}
			if len(words) == 0 && !snippetSet {
				snippet = cut(m.Text, 120)
				snippetSet = true
			}
			for i, w := range words {
				idx := strings.Index(lower, w)
				if idx >= 0 {
					matched[i] = true
					if !snippetSet {
						start := max(0, utf8.RuneCountInString(lower[:idx])-40)
						snippet = excerpt(m.Text, start, utf8.RuneCountInString(w)+80)
						snippetSet = true
					}
				}
			}
		}
		match := true
		for _, ok := range matched {
			if !ok {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		s.Snippet = cut(snippet, 120)
		s.Messages = nil
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ended == out[j].Ended {
			return out[i].Key < out[j].Key
		}
		return out[i].Ended > out[j].Ended
	})
	total := len(out)
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	offset := min(max(q.Offset, 0), total)
	return out[offset:min(offset+limit, total)], total, nil
}
func Markdown(s Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- 项目：%s\n- 会话：%s\n- 时间：%s — %s\n- 来源：%s\n- 格式：%s（坏行 %d，超长行 %d）\n\n", mdLabel(s.Title), mdLabel(s.Project), mdLabel(s.ID), mdLabel(s.Started), mdLabel(s.Ended), mdLabel(s.Source), mdLabel(s.Status), s.BadLines, s.Oversized)
	for _, entry := range []struct {
		label  string
		values map[string]int
	}{{"未知记录类型", s.Unknown}, {"省略的内容", s.Omitted}} {
		if len(entry.values) > 0 {
			keys := []string{}
			for k := range entry.values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(&b, "%s：", entry.label)
			for _, k := range keys {
				fmt.Fprintf(&b, "%s × %d；", mdLabel(k), entry.values[k])
			}
			b.WriteString("\n\n")
		}
	}
	for _, m := range s.Messages {
		role := "用户"
		if m.Role == "assistant" {
			role = "Claude"
		}
		if m.Sidechain {
			role += " · 子任务"
		}
		longest, run := 0, 0
		for _, r := range m.Text {
			if r == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
		fence := strings.Repeat("`", max(3, longest+1))
		fmt.Fprintf(&b, "## %s · %s\n\n%stext\n%s\n%s\n\n", role, mdLabel(m.Timestamp), fence, m.Text, fence)
	}
	return b.String()
}

func mdLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#").Replace(s)
}
