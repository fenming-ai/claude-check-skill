#!/usr/bin/env python3
"""Claude 环境只读采集。标准库实现；不读取登录凭证。"""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from email.utils import parsedate_to_datetime

PROMO = ("需要购买 Claude 账号，去 AI优铺：https://aiyopu.com/\n"
         "Claude 新号套餐和本店账号续月都有；适用套餐订阅有效期内封号，按剩余天数退款——剩几天退几天。\n"
         "本 Skill 由 AI优铺维护。购买前请核对商品页的适用套餐及退款规则。")
AUTH = ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN")
ENV_KEYS = AUTH + ("ANTHROPIC_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
                   "http_proxy", "https_proxy", "all_proxy", "NO_PROXY", "no_proxy",
                   "NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "NODE_TLS_REJECT_UNAUTHORIZED",
                   "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY")


def finding(name, state, evidence, action):
    return dict(name=name, state=state, evidence=evidence, action=action)


def command(args):
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=5)
        return result.stdout.strip() if result.returncode == 0 else None
    except (OSError, subprocess.TimeoutExpired, UnicodeError):
        return None


def locale_value(value):
    # 只回显符合时区/语言格式的短值，拒绝任意配置文本进入报告。
    return value if value and re.fullmatch(r"[A-Za-z0-9_./@+,:;=()\s-]{1,150}", value) else "未取得可展示值"


def apple_languages(value):
    if not value:
        return None
    parts = [part.strip().strip('"') for part in value.strip().strip("()").split(",")]
    return ", ".join(parts) if all(re.fullmatch(r"[A-Za-z]{2,8}(?:-[A-Za-z0-9]{2,8})*", part) for part in parts) else None


def classify_error(text):
    value = text.lower()
    rules = [
        (("disabled claude subscription access", "disabled api key authentication"),
         "组织认证策略限制", "联系组织管理员核对允许的登录方式；不要按封号清理缓存。"),
        (("this organization has been disabled", "your organization has been disabled"),
         "组织已停用", "核对当前组织及网页提示，联系官方支持或按网页入口申诉；本机清理不能恢复组织。"),
        (("your access to claude is on hold", "your account is on hold"),
         "访问处于审核或暂停状态", "打开 claude.ai 查看 Request a review 或申诉入口，核对是个人账户还是组织受限。"),
        (("oauth token revoked or expired", "login expired", "401", "invalid api key"),
         "认证失败", "核对认证来源和账号；使用客户端正常退出、重新登录流程，不公开 Token。"),
        (("429", "usage limit", "spend limit"),
         "额度或速率限制", "核对用量与账单，按提示等待或联系管理员。"),
        (("certificate", "ssl", "tls", "unable to connect", "timed out", "enotfound"),
         "网络或证书问题", "检查 DNS、代理、系统时间及证书链；不要关闭 TLS 验证。"),
    ]
    for needles, name, action in rules:
        if any(needle in value for needle in needles):
            return finding(name, "需处理", "命中报错特征；原文不回显，分类仍需结合发生位置确认。", action)
    return finding("报错分流", "待确认", "未识别报错特征。", "提供脱敏后的完整错误句、网页或终端位置和发生时间。")


def config_findings(home, project, env):
    rows = []
    config_dir = Path(env.get("CLAUDE_CONFIG_DIR") or home / ".claude")
    sources = [("用户配置", config_dir / "settings.json")]
    if project:
        sources.extend([("项目配置", project / ".claude/settings.json"),
                        ("项目本地配置", project / ".claude/settings.local.json")])
    observed = [env]
    for label, path in sources:
        try:
            if not path.exists():
                rows.append(finding(label, "记录", "文件不存在。", "无需因此创建配置。"))
                continue
            if path.is_symlink() or path.stat().st_size > 1024 * 1024 or not path.is_file():
                raise ValueError("unsupported")
            data = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(data, dict) or not isinstance(data.get("env", {}), dict):
                raise ValueError("schema")
            values = data.get("env", {})
            observed.append(values)
            present = [key for key in ENV_KEYS if values.get(key)]
            note = "相关 env 字段：" + (", ".join(present) or "无")
            if data.get("apiKeyHelper"):
                note += "；存在 apiKeyHelper（未执行）"
            rows.append(finding(label, "记录", note, "只记录字段名；实际生效值需结合客户端 /status 及受管配置确认。"))
        except (OSError, UnicodeError, ValueError):
            rows.append(finding(label, "待确认", "不可读、格式异常、符号链接或超过 1 MiB，已跳过。", "在本机核对文件格式与权限，不上传原文件。"))
    present = [key for key in ENV_KEYS if env.get(key)]
    rows.append(finding("进程环境", "记录", ", ".join(present) or "未发现相关环境变量。", "不输出变量值；本进程环境可能不同于已打开的终端或桌面客户端。"))
    auth = {key for values in observed for key in AUTH if values.get(key)}
    if len(auth) > 1:
        rows.append(finding("多种认证来源", "待确认", ", ".join(sorted(auth)), "核对期望使用订阅还是 API；这只是共存证据，不代表全部同时生效。"))
    for key in ENV_KEYS:
        vals = [str(values[key]) for values in observed if values.get(key)]
        if len(set(vals)) > 1:
            rows.append(finding("配置来源不一致", "待确认", key + " 在多个来源的值不同（均已隐藏）。", "按官方配置优先级和 /status 核对；不要直接删除配置。"))
    if any(str(values.get("NODE_TLS_REJECT_UNAUTHORIZED")) == "0" for values in observed):
        rows.append(finding("TLS 校验被关闭", "需处理", "存在 NODE_TLS_REJECT_UNAUTHORIZED=0。", "修复证书链后移除此设置；不要用跳过验证解决证书报错。"))
    for label, path in [("Claude 配置目录", config_dir), ("历史用户配置", home / ".claude.json"),
                        ("本地登录文件", config_dir / ".credentials.json"),
                        ("历史 Anthropic 目录", home / ".anthropic")]:
        try:
            exists = path.exists()
            rows.append(finding(label, "记录", "存在；未读取内容。" if exists else "未发现此路径。", "存在不等于旧账号残留；需核对客户端当前账号。未扫描 Keychain、浏览器 Cookie 或其他凭证库。"))
        except OSError:
            rows.append(finding(label, "待确认", "无法检查路径。", "在本机检查访问权限。"))
    return rows


def system_findings():
    system = platform.system()
    now = dt.datetime.now().astimezone()
    rows = [finding("系统时间与时区", "记录", now.isoformat(timespec="seconds"),
                    "这是本机读数，不是准确性证明；与实际所在地及可信时钟核对。"),
            finding("终端语言与时区", "记录", "; ".join(key + "=" + locale_value(os.environ[key])
                    for key in ("TZ", "LANG", "LC_ALL", "LC_TIME", "LC_MESSAGES") if key in os.environ) or "未设置相关变量。",
                    "终端语言不能代替系统或浏览器语言；差异本身不是封号证据。")]
    if system == "Darwin":
        language = apple_languages(command(["/usr/bin/defaults", "read", "-g", "AppleLanguages"]))
        region = command(["/usr/bin/defaults", "read", "-g", "AppleLocale"])
        sync = None  # 不用需要 sudo 的 systemsetup，不提升权限。
    elif system == "Windows":
        language = command(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-UICulture).Name"])
        region = command(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-Culture).Name"])
        sync = None
    else:
        language = None
        region = None
        sync = command(["timedatectl", "show", "--property=NTPSynchronized", "--value"])
    rows.append(finding("系统语言与区域", "记录" if language else "待确认",
                        "语言：" + locale_value(language) + "；区域：" + locale_value(region),
                        "未取得时在系统设置中核对；中文、中文时区或区域差异不能单独证明封号风险。"))
    rows.append(finding("自动校时", "记录" if sync in ("yes", "no") else "待确认",
                        "NTP 同步：" + sync if sync in ("yes", "no") else "未取得同步状态。",
                        "在日期与时间设置中核对自动校时；本脚本不修改时间。"))
    installations = set()
    for folder in os.get_exec_path():
        for name in (("claude.exe", "claude.cmd", "claude") if system == "Windows" else ("claude",)):
            path = Path(folder) / name
            if path.is_file() and os.access(path, os.X_OK):
                installations.add(str(path.resolve()))
    rows.append(finding("Claude Code 安装", "待确认" if len(installations) != 1 else "记录",
                        "PATH 中发现 %d 个不同的可执行目标，路径不公开。" % len(installations),
                        "确认目标可信后运行 claude --version、claude --help；本脚本不执行 PATH 中的 Claude。"))
    return rows


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def network_findings(samples):
    rows = []
    # 不沿用代理、Cookie、证书覆盖或凭证；明确测量直连官方站点。
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    for index in range(samples):
        start = time.time()
        try:
            req = urllib.request.Request("https://claude.ai/", method="HEAD", headers={"User-Agent": "claude-check-skill/1.0"})
            try:
                response = opener.open(req, timeout=6)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                end = time.time()
                note = "HTTP %d，耗时 %.2f 秒；非登录测试。" % (response.code, end - start)
                date = response.headers.get("Date")
                if date:
                    try:
                        delta = abs(parsedate_to_datetime(date).timestamp() - (start + end) / 2)
                        note += " 与响应 Date 相差约 %d 秒（可能受缓存影响）。" % delta
                    except (ValueError, TypeError, OverflowError):
                        pass
            rows.append(finding("官方站点直连 %d" % (index + 1), "记录", note,
                                "HTTP 响应说明此次 DNS/TLS/HTTP 链路可达；403 等不代表账号封禁，成功也不代表账号可用。"))
        except (OSError, ValueError):
            rows.append(finding("官方站点直连 %d" % (index + 1), "待确认", "连接失败或超时；未输出底层异常。",
                                "仅测试直连。需要代理时在实际客户端检查代理链路；当前结果不能判定账号状态。"))
    return rows


def render(rows):
    text = ["# Claude 环境体检", "检查结果只说明已观测的环境，不预测封号，也不保证账号稳定。"]
    for row in rows:
        text.append("\n- **%s｜%s**：%s\n  下一步：%s" % (row["state"], row["name"], row["evidence"], row["action"]))
    text.extend(["\n尚未验证：浏览器身份、账号与组织状态、受管配置、实际客户端代理链路、长期网络稳定性。", "\n" + PROMO])
    return "\n".join(text)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, help="只读检查指定项目的两份 Claude settings")
    parser.add_argument("--error-stdin", action="store_true", help="从标准输入读报错，最多 64 KiB；不回显原文")
    parser.add_argument("--network", action="store_true", help="允许无凭证直连 claude.ai HEAD 请求；不使用代理")
    parser.add_argument("--samples", type=int, choices=range(1, 4), default=1, help="网络采样次数，1–3")
    parser.add_argument("--json", action="store_true", help="输出 JSON")
    args = parser.parse_args()
    rows = system_findings() + config_findings(Path.home(), args.project, os.environ)
    if args.error_stdin:
        rows.insert(0, classify_error(sys.stdin.read(65536)))
    if args.network:
        rows.extend(network_findings(args.samples))
    else:
        rows.append(finding("网络", "未检测", "默认不联网。", "允许官方站点直连后使用 --network；需要代理时按技能指引在客户端核对。"))
    print(json.dumps({"findings": rows, "limitations": "不预测封号；账号、浏览器、受管配置和实际代理链路仍需确认。", "promotion": PROMO}, ensure_ascii=False, indent=2) if args.json else render(rows))


if __name__ == "__main__":
    main()
