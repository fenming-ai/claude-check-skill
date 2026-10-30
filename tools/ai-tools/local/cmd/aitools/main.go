package main

import (
	"aitools/internal/fsguard"
	"aitools/internal/netdiag"
	"aitools/internal/server"
	"aitools/internal/sessions"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
)

var version = "0.1.0-preview"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func output(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
func run() error {
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "serve")
	}
	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
		port := fs.Int("port", 0, "本机端口")
		if e := fs.Parse(os.Args[2:]); e != nil {
			return e
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		return server.Serve(ctx, *port, version, func(url string) {
			fmt.Println("本机工具地址：", url)
			fmt.Println("关闭此窗口即退出；请勿分享带令牌的地址。")
			if !*noOpen {
				var cmd *exec.Cmd
				switch runtime.GOOS {
				case "darwin":
					cmd = exec.Command("open", url)
				case "windows":
					cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
				}
				if cmd != nil {
					if e := cmd.Run(); e != nil {
						fmt.Fprintln(os.Stderr, "无法自动打开浏览器，请复制上面的本机地址。")
					}
				}
			}
		})
	case "net":
		fs := flag.NewFlagSet("net", flag.ContinueOnError)
		project := fs.String("project", "", "项目目录")
		offline := fs.Bool("offline", false, "不发送请求")
		fs.Bool("json", false, "JSON 输出")
		if e := fs.Parse(os.Args[2:]); e != nil {
			return e
		}
		if *project != "" {
			resolved, e := fsguard.Root(*project)
			if e != nil {
				return e
			}
			*project = resolved
		}
		return output(netdiag.Collect(context.Background(), *project, !*offline, version))
	case "sessions":
		fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
		root := fs.String("root", "", "会话记录目录")
		q := fs.String("q", "", "关键词")
		asJSON := fs.Bool("json", false, "JSON 输出")
		if e := fs.Parse(os.Args[2:]); e != nil {
			return e
		}
		if *root == "" {
			return fmt.Errorf("请用 --root 指定记录目录")
		}
		items, sum, e := sessions.Scan(context.Background(), *root)
		if e != nil {
			return e
		}
		results, total, e := sessions.Search(items, sessions.Query{Q: *q, Limit: 200})
		if e != nil {
			return e
		}
		if *asJSON {
			return output(map[string]any{"summary": sum, "total": total, "items": results})
		}
		for _, s := range results {
			fmt.Printf("%s  %s  %s\n", s.Ended, s.Project, s.Title)
		}
		return nil
	default:
		return fmt.Errorf("未知命令")
	}
}
