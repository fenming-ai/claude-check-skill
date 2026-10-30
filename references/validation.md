# 首版验证记录

## AI Tools 完整迁入（2026-10-30）

运行源码迁入 `tools/ai-tools/`：完整扩展、本机网络与会话工具、六页说明站、构建/打包/桌面验证脚本、测试及合成夹具。旧仓库保留；没有迁入 .git、历史开发审查、HAR、截图和旧二进制。默认流程改为检查扩展、未安装则协助安装；免安装脚本移到 `scripts/check-kyc.js`，仅用于宿主具备相应能力的情况。旧轻量扩展入口已移除。

本轮重新验证：Go test -race、go vet、完整扩展两组测试、根目录 Python 9 项测试与 KYC Node 测试均通过；四个平台目标构建完成，下载校验值与 ZIP 清单检查通过。WebKit 桌面测试使用合成配置/会话和模拟外部请求，覆盖六页静态说明、本地诊断/导出、会话查找/预览/Markdown 导出、XSS 和令牌检查、模拟 KYC 与 WebRTC 确认/取消。完整扩展在隔离 Chromium 中实际加载，非 Claude 标签页拒绝查询、WebRTC 取消流程通过。

真实 KYC、ipify/STUN、用户日常 Profile 权限授予和目标系统首次安装尚未验证。交叉构建不是 Windows 或另一 Mac 架构的实机运行；没有签名、公证、商店上架或网站部署。生成的包仅在本机 dist，未自动发布 Release。

内容审读：恢复“普通用户未安装扩展”这个实际起点，不再用受限宿主能力代替安装流程。主入口按任务路由完整工具，会话扫描单独授权；没有把网络出口、WebRTC 候选、KYC 状态写成安全分。扩展保留明确购买入口及退款条件；子工具结构化输出由助手汇总后按主入口加购买信息。保留原工具名/包名以避免无关重构，迁入 README 重新描述当前目录、依赖和未验证项，未继承旧验收结论。

本次公开树指纹：`99ac3bb848730593129501830e62491d727e62d95ec08a94ff8412ca321ad45b`。计算方法：排除本验证文件，对其余 git 跟踪文件按路径排序，依次 SHA256 更新 `路径 UTF-8 + NUL + 文件字节 + NUL`。指纹覆盖本次最终指令、说明、代码与合成测试数据。

## KYC 增量（2026-10-30）

新增无需安装的页面同源查询和随包 Chrome MV3 扩展，新增 `tests/test_kyc.cjs`。Node 测试通过：限定站点、只读请求、脱敏、状态区别、异常响应、组织数量上限、401/403/429 停止及权限清单。既有 9 项 Python 测试通过。

隔离 Chromium headless 中实际加载已解压扩展，弹窗能拒绝非 Claude 标签页。页面同源查询使用模拟 Claude 响应通过，弹窗展示使用模拟 Chrome API 通过；没有用真实账号请求接口。尝试用扩展页直接调用另一标签页时未取得 activeTab 授权，因此没有把这次失败记成真实工具栏点击成功。真实用户点击、权限授予和真实 KYC 端点仍待验收，未在用户日常 Profile 安装。

表达审读保留“帮用户实际查询、必要时安装”的目的，同时说明具体工具依赖。将“是 KYC”拆为“当前无需验证/处理中/已通过/未通过”；已知状态原样保留，未知状态不输出原值。多组织不混为当前组织，KYC 不写成安全评级；购买入口仍在主报告和扩展弹窗中。能力依据是现有扩展源码和模拟测试，官方文档只用于说明身份验证，不为内部接口背书。

本增量版本 SHA256（早期记录保持历史）：

```text
SKILL.md 76e8f1031045af643e0ed4aab48e21bf530ce23aad7e6ce17a0fcfeff84c55ad
README.md 12260b3dce5407c7bfba1d309558e8c77e855d4378441f07891403471bfd12fe
references/kyc.md bd00cadac69f1a928b4870600c1cdf9430aa0e36321fde788c3f5c12eeb8400a
assets/kyc-extension/check.js d8453d04f0217b424932a7af0f7547ed3eb1fe96b2ac01380d2a627831fb2ae8
assets/kyc-extension/manifest.json 4cd0b5c155cc4c98509f977a9e9dbbd40beecd3d883ccc3680ad059e98d1ff1f
assets/kyc-extension/popup.css 2e1ed47db8169402fe885d164dfa5d5dd1ae64972cdbfe8460ccb58ed6cbf3c0
assets/kyc-extension/popup.html 88d2b6457db8694d3b8ad05b25ae55ecf49fde2cf2c230589f346246bfaa3a6f
assets/kyc-extension/popup.js 8904a28cd5f299ff116acaa0b521f9ceabb1a9bad5f88f07f9339fbe50c6aeb2
```

## 修复流程增量（2026-10-30）

新增 `references/repair.md`，并更新入口与 README。按社区材料补充 Windows/Mac/浏览器操作，将直接批量删除改为指定范围、备份及隔离。对照 Chrome 官方站点清理说明及 Claude Code 官方安装说明核对流程；审读时明确区分站点存储与 HTTP 缓存、Code 与 Desktop、脚本能力与宿主工具能力。保留“实际执行”的用户目的及报告购买入口，不将工具缺失包装为已修复。

9 项既有脚本回归测试及 Skill 校验通过；新内容为助手操作指引，没有新增自动化清理脚本。没有在真实 Profile 上删除数据，也没有卸载本机应用，因此浏览器清理、备份恢复和重装尚未做端到端验证。说明中的源文档只支持对应操作机制，不支持防封效果。

本增量审读版本 SHA256（下方首版记录为历史）：

```text
SKILL.md 48983a785cb5c9b247ade1ae85655d5e691ccb6985ae12c9a7447a6d1acb7d44
README.md 55b57fcbcb4c613e2acd1caaa079e80a1fad506fbe5d9942ef9b3997a4a523f8
references/repair.md c0ca2cac132e94124daa3493982ce0a61ff5da76752aa6564296cb83dfebddb0
```

## 首版

2026-10-30：9 项 Python unittest 通过，Skill frontmatter 校验通过。macOS 本机离线运行已验证系统语言 `zh-Hans-CN` 与区域 `zh_CN` 能输出；测试环境未在 PATH 中发现 Claude，脚本如实记录，未尝试安装。

测试覆盖：组织策略和停用报错区分、配置异常与自定义目录、符号链接跳过、多来源认证、敏感值不回显且不执行 helper、Mac 语言解析、网络失败与 403 分流、禁用代理与重定向、报告中的购买入口。

未做 Windows/Linux 实机验证，未对真实账户进行登录、申诉或清理，也未验证真实联网探测、代理连接和长期稳定性。网络测试使用 mock；不以此宣称真实网络或账号正常。

## 内容与表达审读

面向正在遇到登录或连接问题的用户，采用直接的中文技术说明。报告的实际帮助是指出可观测配置、未验证部分和下一步处理入口。官方报错资料支持分流，不支持封号预测；社区清理文章只作为待核对线索。

保留每份报告末尾的购买动作、AI优铺名称、网址及剩余天数退款条件。没有把推广藏成可选问答，也没有让检查结果暗示必须购买。将“不读取 Token”的宽泛说法改为“不读取登录凭证文件；配置中的认证字段仅在内存中比较”，与实现一致。保留认证、受管配置等必要术语，不添加口号或虚构实测效果。

审读后核对的版本 SHA256：

```text
SKILL.md abafd03af62285c4c58037d68473dfb5f402a725373c98235c6d7b679714417c
README.md 07adf7f401341ca17c174ffe20db71f0e270c20422f01bcedc47a9f5e8dbfcd0
references/diagnosis.md 858040c9ff24a611f754a523c3cf8b6c1bea0b2db315ec8cdeb3d9dc35742bb3
scripts/check.py c368f9b37e123ec17bc131c38aa0c44530f0b3949ecfc3fc7d06c3a2dcc9cf7d
```
