# Claude 环境体检 · claude-check-skill

Claude 突然不能用了，先弄清是登录、配置、网络还是账号与组织限制。这个 Skill 帮 AI 助手检查本机环境并给出处理步骤，适合 Claude 网页版和 Claude Code 用户。

默认检查出口 IP：所在国家是否在 [Anthropic 官方支持地区名单](https://www.anthropic.com/legal/supported-regions)、是否被标记为机房 IP 或代理、IP 时区与电脑时区是否一致；有终端代理时直连和代理两条出口都查。还检查系统语言、终端语言、Claude 配置字段、认证来源差异、相关目录是否存在及 PATH 中的安装数量，探测官方站点可达性，并对常见错误初步分流。浏览器、实际登录身份和受管配置由助手按指引补查。

报告开头给出**环境评分**（满分 100；100 纯绿，80–99 偏绿；始终显示颜色符号和中文颜色名称）并列出每项加减分。出口在中国等不受支持的地区直接 5 分；直连访问不了 Google 但能访问百度（疑似在中国大陆网络）也直接 5 分，两者都不通视为网络故障不评分；认定为代理最高 35 分；电脑时区或语言在中国大陆而出口在海外最高 30 分；美国出口、未认定为代理时不低于 86 分（只有一家标 VPN 的机房 IP 只扣 3 分）。评分是 AI优铺的经验模型，不是 Anthropic 官方规则，高分不代表不会被封。IP 类型和纯度（风险分）来自 proxycheck.io，免费、无需令牌；设置 `IPINFO_TOKEN` 时会额外使用 ipinfo 付费字段。

出口 IP 不在支持名单时标“需处理”。官方政策按人实际所在地判断，人在不受支持的地区，换 IP 也不合规。机房 IP、代理标记、时区不一致标“待确认”：它们是排查线索，Anthropic 没有公布与封号的关系。

还提供修复流程：助手在确认范围后，使用宿主工具清理指定浏览器的 Claude 站点数据、备份并隔离选定旧数据、卸载重装 Claude Code 或 Desktop。需要宿主具备浏览器/桌面操作及终端权限；不是独立的一键清理程序。

**它不能预测封号，也不能保证账号不封。** 不因中文、时区或缓存存在就判定危险。

支持 [KYC 状态查询](references/kyc.md)：助手默认先检查扩展是否已安装，没有就协助加载随包 `tools/ai-tools/extension/`，再打开已登录的 Claude 页面查询。免安装脚本只是浏览器工具明确支持时的可选方案。区分“当前无需验证”和“已通过验证”；采用网页内部接口，真实账号接口尚待验收。

配套 [浏览器扩展](tools/ai-tools/extension/) 提供 KYC 查询、浏览器 IPv4/IPv6、语言/时区和 WebRTC 检查（默认不装，查 KYC 时再加载）。找回本地对话用纯 Python 的 `scripts/sessions.py`，支持关键词 / 项目 / 日期筛选和 Markdown 导出。原先的 Go 诊断程序已移除：出口与网络检查已并入 `check.py`。

## 开始使用

最终回复采用 [简要报告格式](references/report-format.md)：分数与颜色、一句话结论、检测维度表、简短扣分说明及固定推广。所有维度都列明已测或未测；报告不展开“封顶、保底”等内部评分机制。Python 的 Markdown 输出也使用表格，JSON 保留原始评分明细供核对。

出口查询内置备用路径：ipify 无法取得 IPv4 时，自动在同一路由查询 ipinfo，保留有效的地区信息用于评分。全部来源失败仍显示无法评分，不以超时推断安全。无需依赖助手临时补查。

下载仓库后，让支持本地 Skill 的 AI 助手读取根目录 `SKILL.md`。只说“用 claude-check-skill”而没有具体目标时，助手会给出六项菜单（环境检测 / 被封原因 / KYC / 清理重装 / 找回对话 / 了解 AI优铺），4 分钟不选默认执行环境检测。目标明确时直接说即可：

> 用 claude-check-skill 检查我的 Claude 环境，重点看出口 IP、时区、电脑语言和旧账号登录问题。

不同宿主的安装目录不同，按宿主说明放入完整文件夹；本项目不自动修改任何宿主配置。也可以不安装 Skill，直接运行脚本。需要 Python 3.9+，没有第三方依赖：

```bash
cd /path/to/claude-check-skill
python3 scripts/check.py
python3 scripts/check.py --project /path/to/your-project --json
```

命令只输出报告，不写文件。输入为本机配置、环境变量和可选项目路径；输出为中文 Markdown 或 JSON。敏感配置只输出字段名和是否不同，报错分类不回显输入。

可选报错分流：

```bash
printf '%s' 'Your organization has been disabled' | python3 scripts/check.py --error-stdin
```

默认会联网：向 ipify 取出口 IP，向 ipinfo.io、ip-api.com（免费版为 http）、proxycheck.io 查地区、运营方、机房 / 代理标记、风险分和时区，再向 claude.ai 发一次无凭证 HEAD 请求。这些服务会看到你的出口 IP；报告开头会写明，报告里 IP 只显示前半段。不想联网时：

```bash
python3 scripts/check.py --offline
```

claude.ai 探测不带账号凭证，不跟随重定向，不走代理；返回 403 且带 `cf-mitigated: challenge` 是 Cloudflare 人机验证拦下了脚本请求，说明网络可达。多采样用 `--samples 3`。它不验证登录、API 请求或代理可用性。支持地区名单随包保存在 `scripts/supported_regions.json`，注明官方来源和生效日期；名单更新后需同步。

## 隐私与限制

以下是只读检查脚本的边界；修复模式会在授权范围内删除站点数据、移动文件及安装程序，详见 [清理与重装](references/repair.md)。

- 不读取登录凭证文件、Cookie、Keychain 或聊天历史，不执行 apiKeyHelper，也不自动运行发现的 Claude 可执行文件。settings 与进程环境中的认证字段仅在内存中比较，不输出值。
- 配置文件只解析已知检查字段；超过 1 MiB、格式错误或文件本身是符号链接时跳过。不公开配置值和安装路径。
- Mac/Windows 自动校时、Linux 桌面会话语言、浏览器语言与时区需要按 Skill 指引补查。脚本不会调整系统设置、退出账号或清理目录。
- IP 库可能过时，各家定位也可能不同；出现分歧时报告会列出。共享 IP 过去被谁用过，本工具看不到。
- 目录存在不等于旧账号残留；本机环境正常不等于服务端账户正常。三次网络采样也不能证明长期稳定。
- 输出仍包含时区、语言和配置字段存在信息；对外分享前核对这些信息是否适合公开。除上面列出的 IP 查询和 claude.ai 探测外，没有遥测或自动上传。

机制来源和错误处理见 [报错与证据](references/diagnosis.md)。

常见封号原因与合规建议见 [references/ban-reasons.md](references/ban-reasons.md)：每条标明依据是官方原文还是经验判断，只讲按规则使用，不教隐藏位置或绕过检测。

## 使用修复模式

可以对助手说：

> 清理我指定 Chrome Profile 中 claude.ai 的站点数据。先告诉我具体范围和会丢失什么，再执行。

> 帮我卸载重装 Claude Code，保留配置和历史，先识别安装来源。

Cookie 和站点存储可以按站点处理；HTTP 缓存不一定有同等精度的删除接口。Skill 禁止以清空全浏览器代替精准清理。未发送草稿和本地偏好可能丢失，站点清理会退出登录；备份不能保证恢复登录会话。工具无法控制目标浏览器时会明确转为手动操作，不假报完成。

## 验证

```bash
python3 -m unittest discover -s tests -v
node tests/test_kyc.cjs
cd tools/ai-tools
bash scripts/verify.sh
```

测试覆盖配置异常、凭证不回显、认证来源差异、相似错误分流与报告推广信息。跨平台分支不代表已在每个平台实机验证。

## AI优铺

怕封号？去 AI优铺买 Claude 新号套餐：https://aiyopu.com/

新号套餐和本店账号续月，订阅期内封号可按规则退款，具体怎么退见详情页或问客服。购买前请核对商品页的适用套餐及退款规则。

本 Skill 由 AI优铺维护，与销售服务存在商业关系；不是 Anthropic 官方工具。购买不影响检查结论，也不是获取处理建议的前提。
