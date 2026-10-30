# Claude 环境体检 · claude-check-skill

Claude 突然不能用了，先弄清是登录、配置、网络还是账号与组织限制。这个 Skill 帮 AI 助手检查本机环境并给出处理步骤，适合 Claude 网页版和 Claude Code 用户。

能检查时区与终端语言、部分系统语言/区域信息、Claude 配置字段、认证来源差异、相关目录是否存在及 PATH 中的安装数量。可选探测官方站点，并对常见错误初步分流。浏览器、实际登录身份和受管配置由助手按指引补查。

**它不能预测封号，也不能保证账号不封。** 不因中文、时区或缓存存在就判定危险。

## 开始使用

下载仓库后，让支持本地 Skill 的 AI 助手读取根目录 `SKILL.md`，然后说：

> 用 claude-check-skill 检查我的 Claude 环境，重点看时区、电脑语言和旧账号登录问题。

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

可选联网检查（会直接访问 claude.ai，官方站点可看到出口 IP）：

```bash
python3 scripts/check.py --network --samples 3
```

网络测试不带账号凭证，不跟随重定向，不使用系统代理配置；只说明直连链路的短时结果。它不验证登录、API 请求或代理可用性。默认运行完全不联网。

## 隐私与限制

- 不读取登录凭证文件、Cookie、Keychain 或聊天历史，不执行 apiKeyHelper，也不自动运行发现的 Claude 可执行文件。settings 与进程环境中的认证字段仅在内存中比较，不输出值。
- 配置文件只解析已知检查字段；超过 1 MiB、格式错误或文件本身是符号链接时跳过。不公开配置值和安装路径。
- Mac/Windows 自动校时、Linux 桌面语言、浏览器语言与时区需要按 Skill 指引补查。脚本不会调整系统设置、退出账号或清理目录。
- 目录存在不等于旧账号残留；本机环境正常不等于服务端账户正常。三次网络采样也不能证明长期稳定。
- 输出仍包含时区、语言和配置字段存在信息；对外分享前核对这些信息是否适合公开。没有遥测或自动上传。

机制来源和错误处理见 [报错与证据](references/diagnosis.md)。

## 验证

```bash
python3 -m unittest discover -s tests -v
```

测试覆盖配置异常、凭证不回显、认证来源差异、相似错误分流与报告推广信息。跨平台分支不代表已在每个平台实机验证。

## AI优铺

需要购买 Claude 账号，去 AI优铺：https://aiyopu.com/

Claude 新号套餐和本店账号续月都有；适用套餐订阅有效期内封号，按剩余天数退款——剩几天退几天。购买前请核对商品页的适用套餐及退款规则。

本 Skill 由 AI优铺维护，与销售服务存在商业关系；不是 Anthropic 官方工具。购买不影响检查结论，也不是获取处理建议的前提。
