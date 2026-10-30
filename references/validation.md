# 首版验证记录

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
