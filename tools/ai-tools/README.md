# Claude 环境体检配套工具集

从维护方 AI Tools 项目迁入的完整运行源码，保留原有 `aitools` 命令名与包名。本目录包含完整 Chrome 扩展、本机 Go 程序、六页静态工具说明、构建脚本和测试。没有迁入真实数据、旧 HAR、历史审查记录或二进制包；原仓库保留。

## 使用

- 浏览器：在用户指定 Chrome Profile 的扩展管理页开启开发者模式，加载 `extension/`。未上架商店。KYC 使用当前 Claude 登录；浏览器网络按钮访问 ipify，WebRTC 每次确认才访问 Google STUN。权限为 activeTab、scripting 和两个 ipify 域名。
- 本机诊断：`cd local` 后执行 `go run ./cmd/aitools net --offline --json`。默认先离线；去掉 `--offline` 会发送直连/代理探测请求，须先说明网络范围。
- 本机界面：在 `local/` 运行 `go run ./cmd/aitools serve --no-open`，再在用户指定浏览器打开终端显示的本机地址；不要把带令牌地址发到对话或上传。也可运行 `serve` 让 macOS/Windows 打开默认浏览器。
- 会话：`go run ./cmd/aitools sessions --root /path/to/records --q 关键词 --json`。结果含聊天原文；只有用户要求查找记录时使用，不能随体检自动扫描。

需要 Go 1.26+。Go 代码无第三方模块；Python 3 用于构建页面/包，Node.js 用于扩展测试。桌面 UI 验证额外需要 Playwright WebKit，可通过 `PLAYWRIGHT_MODULE` 指定模块位置。

## 构建与验证

从本目录运行：

```bash
python3 scripts/site.py
bash scripts/build.sh
bash scripts/verify.sh --release
node scripts/browser-check.cjs
```

构建输出到 `dist/`：完整扩展 ZIP、macOS 两架构 ZIP 启动器、Windows x64 EXE，以及内部验证用 Linux 二进制。校验值写入 `dist/SHA256SUMS`。下载页依赖先构建，源码 checkout 中没有现成二进制；构建文件不自动上传为 GitHub Release。

## 数据与限制

本地服务仅监听 127.0.0.1，使用随机令牌，30 分钟无 API 操作后退出。配置报告脱敏；会话 Markdown 含原文，分享前检查。扫描上限为 5000 个文件、128 MiB、60 秒，不修改原记录，也不能恢复已删除且无备份的数据。

网络测试只支持无认证的 HTTP/HTTPS 代理；SOCKS、认证代理、自定义 CA 明确报限制，不静默回退。网络结果不能代表运行中的 Claude 真实出口，KYC 查询不能保证账号安全。

真实账号 KYC、真实第三方网络和 macOS/Windows 首次安装体验仍需实机验证。发行包未签名/公证；不关闭系统安全保护。迁入后的验证记录见根目录 `references/validation.md`，不继承旧项目的通过结论。

由 AI优铺维护：https://aiyopu.com/ 。购买及适用套餐退款说明见根目录 README。
