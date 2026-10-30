#!/usr/bin/env python3
"""Claude 环境采集：出口 IP 地区、类型、时区，本机时区与语言，配置与安装。标准库实现；不读取登录凭证。"""
import argparse
import datetime as dt
import ipaddress
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

try:
    from zoneinfo import ZoneInfo
except ImportError:  # 缺少 zoneinfo 时跳过时区比较
    ZoneInfo = None

HERE = Path(__file__).resolve().parent
REGIONS_FILE = HERE / "supported_regions.json"
UA = "claude-check-skill/1.1"
IP_ECHO = (("IPv4", "https://api.ipify.org?format=json"), ("IPv6", "https://api6.ipify.org?format=json"))
PROXY_KEYS = ("HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy")
IP_DISCLOSURE = "本次检查会向 ipify、ipinfo.io、ip-api.com 发送请求，查询出口 IP 的地区、类型和时区；这些服务会看到你的出口 IP。加 --offline 可跳过。"

PROMO = ("需要购买 Claude 账号，去 AI优铺：https://aiyopu.com/\n"
         "Claude 新号套餐和本店账号续月都有；适用套餐订阅有效期内封号可按规则退款，具体怎么退见商品详情页或联系客服。\n"
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


def linux_language():
    """系统级语言：localectl，其次 /etc/locale.conf、/etc/default/locale。桌面会话语言可能不同。"""
    out = command(["localectl", "status"]) or ""
    m = re.search(r"System Locale:\s*(?:LANG=)?([^\s,]+)", out)
    if m:
        return m.group(1)
    for path in ("/etc/locale.conf", "/etc/default/locale"):
        try:
            text = Path(path).read_text(encoding="utf-8", errors="ignore")[:4096]
        except OSError:
            continue
        m = re.search(r'^\s*LANG="?([^"\s]+)"?', text, re.M)
        if m:
            return m.group(1)
    return None


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
        language = linux_language()
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
                if response.code == 403 and str(response.headers.get("cf-mitigated", "")).lower() == "challenge":
                    note += " 这是 Cloudflare 人机验证拦下了脚本请求（命令行请求常见），说明网络已到达 claude.ai；不是账号或 IP 被封的证据。"
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


def mask_ip(ip):
    """报告常被转发，IP 只显示前半段。"""
    try:
        addr = ipaddress.ip_address(ip)
    except ValueError:
        return "无效地址"
    if addr.version == 4:
        return ".".join(ip.split(".")[:3]) + ".x"
    return ":".join(addr.exploded.split(":")[:3]) + ":…"


def get_json(opener, url, timeout=6):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/json"})
    with opener.open(req, timeout=timeout) as response:
        return json.loads(response.read(65536).decode("utf-8"))


def lookup_ip(opener, ip):
    """两家服务交叉核对。返回 [{provider, country, timezone, org, hosting, proxy, mobile}]，取不到的留 None。"""
    rows = []
    try:
        d = get_json(opener, "https://ipinfo.io/%s/json" % ip)
        rows.append(dict(provider="ipinfo.io", country=d.get("country"), timezone=d.get("timezone"),
                         org=d.get("org"), hosting=None, proxy=None, mobile=None))
    except (OSError, ValueError, UnicodeError):
        pass
    try:  # ip-api 免费版只有 http
        d = get_json(opener, "http://ip-api.com/json/%s?fields=status,countryCode,timezone,isp,as,proxy,hosting,mobile" % ip)
        if d.get("status") == "success":
            rows.append(dict(provider="ip-api.com", country=d.get("countryCode"), timezone=d.get("timezone"),
                             org=d.get("as") or d.get("isp"), hosting=d.get("hosting"), proxy=d.get("proxy"),
                             mobile=d.get("mobile")))
    except (OSError, ValueError, UnicodeError):
        pass
    return rows


def load_regions():
    try:
        data = json.loads(REGIONS_FILE.read_text(encoding="utf-8"))
        return data["regions"], data.get("effective", "未知"), data.get("source", "")
    except (OSError, ValueError, KeyError):
        return None, None, None


def utc_text(minutes):
    sign = "+" if minutes >= 0 else "−"
    return "UTC%s%02d:%02d" % (sign, abs(minutes) // 60, abs(minutes) % 60)


def tz_offset(name, now):
    if not ZoneInfo or not name or not re.fullmatch(r"[A-Za-z0-9_+\-/]{1,64}", name):
        return None
    try:
        return int(now.astimezone(ZoneInfo(name)).utcoffset().total_seconds() // 60)
    except Exception:  # 未知时区或缺少 tzdata
        return None


def ip_findings(env, now=None):
    """出口 IP 的地区、类型、时区。默认执行；没有代理变量时测直连，有代理变量时两条路都测。"""
    now = now or dt.datetime.now().astimezone()
    regions, effective, source = load_regions()
    routes = [("直连", urllib.request.ProxyHandler({}))]
    proxy = {k: env[k] for k in PROXY_KEYS if env.get(k)}
    if proxy:
        https = proxy.get("HTTPS_PROXY") or proxy.get("https_proxy") or proxy.get("ALL_PROXY") or proxy.get("all_proxy")
        routes.append(("终端代理", urllib.request.ProxyHandler({"https": https, "http": https})))
    rows, seen = [], {}
    for route, handler in routes:
        opener = urllib.request.build_opener(handler)
        for family, url in IP_ECHO:
            if route != "直连" and family == "IPv6":
                continue  # 代理只测一次 IPv4，避免重复请求
            try:
                ip = get_json(opener, url).get("ip", "")
                ipaddress.ip_address(ip)
            except (OSError, ValueError, UnicodeError, AttributeError):
                if family == "IPv4":
                    rows.append(finding("出口 IP（%s）" % route, "待确认", "未取得 %s 出口地址。" % family,
                                        "检查网络或代理；%s不可用时无法判断出口地区。" % ("代理" if route != "直连" else "")))
                continue
            label = "%s %s" % (route, family)
            if ip in seen:
                rows.append(finding("出口 IP（%s）" % label, "记录", "%s，与%s相同。" % (mask_ip(ip), seen[ip]), "同一个出口，不重复查询。"))
                continue
            seen[ip] = label
            rows.extend(ip_detail(label, ip, lookup_ip(opener, ip), regions, effective, source, now))
    return rows


def ip_detail(label, ip, info, regions, effective, source, now):
    rows = []
    name = "出口 IP（%s）" % label
    countries = sorted({r["country"] for r in info if r.get("country")})
    orgs = sorted({r["org"] for r in info if r.get("org")})
    shown = "%s；定位 %s；运营方 %s（来源 %s）" % (
        mask_ip(ip), "/".join(countries) or "未取得", "/".join(orgs)[:120] or "未取得",
        "、".join(r["provider"] for r in info) or "无")
    if not countries:
        rows.append(finding(name, "待确认", shown, "IP 查询服务未返回地区，稍后重试或换网络再查。"))
        return rows
    if regions is None:
        rows.append(finding(name, "待确认", shown + "；支持地区名单缺失，无法对照。", "确认 scripts/supported_regions.json 存在。"))
    else:
        bad = [c for c in countries if c not in regions]
        if bad:
            rows.append(finding(name, "需处理", shown + "；%s 不在 Anthropic 支持地区名单（%s 生效版）。" % ("/".join(bad), effective),
                                "官方按人实际所在地判断：人在不受支持的地区不能使用 Claude，换 IP 也不改变这一点。"
                                "如果你人在支持地区，说明出口走到了别处，检查代理 / VPN 设置。名单：" + source))
        else:
            note = "；%s 在支持名单内" % "/".join(countries)
            if "UA" in countries:
                note += "（乌克兰部分地区除外）"
            rows.append(finding(name, "记录", shown + note + "。",
                                "名单按人实际所在地判断，IP 在支持地区不代表合规，也不是账号安全证明。"))
    if len(countries) > 1:
        rows.append(finding("IP 定位分歧（%s）" % label, "待确认", "各服务给出的国家不同：%s。" % "/".join(countries),
                            "IP 库可能过时；以你实际所在地和官方页面提示为准。"))
    flags = [r for r in info if r.get("hosting") is not None or r.get("proxy") is not None]
    if flags:
        f = flags[0]
        kinds = [k for k, v in (("机房 IP", f.get("hosting")), ("代理 / VPN", f.get("proxy")), ("移动网络", f.get("mobile"))) if v]
        if f.get("hosting") or f.get("proxy"):
            rows.append(finding("IP 类型（%s）" % label, "待确认", "被 %s 标记为：%s。" % (f["provider"], "、".join(kinds)),
                                "Anthropic 没有公布 IP 类型与封号的关系；记录供排查，不代表一定会被封。"
                                "共享 IP 过去被谁用过，本工具看不到。"))
        else:
            rows.append(finding("IP 类型（%s）" % label, "记录", "%s 未标记为机房或代理%s。" % (f["provider"], "（移动网络）" if f.get("mobile") else ""),
                                "IP 库的判断可能滞后，不是 IP 质量担保。"))
    else:
        rows.append(finding("IP 类型（%s）" % label, "待确认", "未取得机房 / 代理标记。", "ip-api.com 未响应；稍后重试。"))
    local = int(now.utcoffset().total_seconds() // 60)
    zones = {r["timezone"]: tz_offset(r["timezone"], now) for r in info if r.get("timezone")}
    zones = {k: v for k, v in zones.items() if v is not None}
    if zones:
        desc = "系统时区 %s；IP 所在时区 %s" % (utc_text(local), "、".join("%s（%s）" % (k, utc_text(v)) for k, v in sorted(zones.items())))
        if local in zones.values():
            rows.append(finding("时区与 IP（%s）" % label, "记录", desc + "，一致。", "无需处理。"))
        else:
            rows.append(finding("时区与 IP（%s）" % label, "待确认", desc + "，不一致。",
                                "系统时区应与你实际所在地一致。所在地对、IP 不对，说明出口在别处（代理 / VPN）；"
                                "IP 对、所在地对但系统时区不对，在系统设置里改回所在地时区并开自动校时。"
                                "官方没有公布时区与封号的关系，这是排查线索。"))
    else:
        rows.append(finding("时区与 IP（%s）" % label, "待确认", "未取得 IP 时区或本机缺少时区数据。", "在系统设置中核对时区和自动校时。"))
    return rows


def render(rows, disclosed=False):
    text = ["# Claude 环境体检", "检查结果只说明已观测的环境，不预测封号，也不保证账号稳定。"]
    if disclosed:
        text.append(IP_DISCLOSURE)
    for row in rows:
        text.append("\n- **%s｜%s**：%s\n  下一步：%s" % (row["state"], row["name"], row["evidence"], row["action"]))
    text.extend(["\n尚未验证：浏览器身份、账号与组织状态、受管配置、实际客户端代理链路、长期网络稳定性。", "\n" + PROMO])
    return "\n".join(text)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, help="只读检查指定项目的两份 Claude settings")
    parser.add_argument("--error-stdin", action="store_true", help="从标准输入读报错，最多 64 KiB；不回显原文")
    parser.add_argument("--offline", action="store_true", help="不联网：跳过出口 IP 查询和 claude.ai 直连")
    parser.add_argument("--network", action="store_true", help="兼容旧参数；联网检查现已默认执行")
    parser.add_argument("--samples", type=int, choices=range(1, 4), default=1, help="claude.ai 直连采样次数，1–3")
    parser.add_argument("--json", action="store_true", help="输出 JSON")
    args = parser.parse_args()
    rows = []
    if args.error_stdin:
        rows.append(classify_error(sys.stdin.read(65536)))
    if not args.offline:
        rows.extend(ip_findings(os.environ))
    rows += system_findings() + config_findings(Path.home(), args.project, os.environ)
    if args.offline:
        rows.append(finding("出口 IP 与网络", "未检测", "使用了 --offline。", "出口 IP 地区、类型、时区是体检重点，建议去掉 --offline 重跑。"))
    else:
        rows.extend(network_findings(args.samples))
    disclosed = not args.offline
    print(json.dumps({"findings": rows, "disclosure": IP_DISCLOSURE if disclosed else None,
                      "limitations": "不预测封号；账号、浏览器、受管配置和实际代理链路仍需确认。", "promotion": PROMO},
                     ensure_ascii=False, indent=2) if args.json else render(rows, disclosed))


if __name__ == "__main__":
    main()
