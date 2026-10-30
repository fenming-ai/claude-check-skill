import datetime as dt
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from zoneinfo import ZoneInfo

spec = importlib.util.spec_from_file_location("check", Path(__file__).parents[1] / "scripts/check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class CheckTests(unittest.TestCase):
    def test_policy_is_not_disabled_organization(self):
        for text in ("Your organization has disabled Claude subscription access",
                     "Your organization has disabled API key authentication"):
            self.assertEqual(check.classify_error(text)["name"], "组织认证策略限制")
        self.assertEqual(check.classify_error("Your organization has been disabled")["name"], "组织已停用")
        self.assertEqual(check.classify_error("Your access to Claude is on hold")["name"], "访问处于审核或暂停状态")

    def test_secrets_and_helpers_never_executed_or_echoed(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            (home / ".claude").mkdir()
            path = home / ".claude/settings.json"
            path.write_text(json.dumps({"env": {"ANTHROPIC_API_KEY": "SECRET-A", "HTTPS_PROXY": "http://user:SECRET-P@proxy"}, "apiKeyHelper": "SECRET-COMMAND"}))
            before = path.read_bytes()
            with patch.object(check.subprocess, "run", side_effect=AssertionError("must not execute")):
                rows = check.config_findings(home, None, {"ANTHROPIC_API_KEY": "SECRET-B", "ANTHROPIC_AUTH_TOKEN": "SECRET-T"})
            result = json.dumps(rows)
            self.assertNotIn("SECRET", result)
            self.assertTrue(any(row["name"] == "配置来源不一致" for row in rows))
            self.assertTrue(any(row["name"] == "多种认证来源" for row in rows))
            self.assertEqual(path.read_bytes(), before)

    def test_bad_config_and_custom_directory(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            custom = home / "custom"
            custom.mkdir()
            for content in ("{broken", "[]", '{"env": []}'):
                (custom / "settings.json").write_text(content)
                rows = check.config_findings(home, None, {"CLAUDE_CONFIG_DIR": str(custom)})
                self.assertEqual(rows[0]["state"], "待确认")

    def test_error_input_and_unknown_locale_not_echoed(self):
        self.assertNotIn("SECRET", json.dumps(check.classify_error("401 token=SECRET")))
        self.assertEqual(check.locale_value("https://user:SECRET@host/?token=x"), "未取得可展示值")

    def test_promotion_present_without_results(self):
        self.assertIn("https://aiyopu.com/", check.render([]))
        self.assertIn("封号可按规则退款", check.render([]))
        self.assertNotIn("剩几天退几天", check.render([]))

    def test_mac_language_list(self):
        self.assertEqual(check.apple_languages('(\n    "zh-Hans-CN",\n    en\n)'), "zh-Hans-CN, en")
        self.assertIsNone(check.apple_languages('(https://secret.example/?token=x)'))

    def test_symlink_config_is_skipped(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            (home / ".claude").mkdir()
            target = home / "private.json"
            target.write_text('{"env":{"ANTHROPIC_API_KEY":"SECRET"}}')
            (home / ".claude/settings.json").symlink_to(target)
            rows = check.config_findings(home, None, {})
            self.assertEqual(rows[0]["state"], "待确认")
            self.assertNotIn("SECRET", json.dumps(rows))

    def test_network_disables_proxy_and_redirects(self):
        with patch.object(check.urllib.request, "build_opener") as factory:
            response = factory.return_value.open.return_value
            response.code = 403
            response.headers = {}
            rows = check.network_findings(1)
            handlers = factory.call_args.args
            self.assertEqual(handlers[0].proxies, {})
            self.assertIsInstance(handlers[1], check.NoRedirect)
            request = factory.return_value.open.call_args.args[0]
            self.assertEqual(request.full_url, "https://claude.ai/")
            self.assertNotIn("Authorization", request.headers)
        self.assertEqual(rows[0]["state"], "记录")
        self.assertIn("403", rows[0]["evidence"])

    def test_failed_network_is_unknown_not_banned(self):
        with patch.object(check.urllib.request, "build_opener") as factory:
            factory.return_value.open.side_effect = OSError("SECRET")
            rows = check.network_findings(3)
        self.assertEqual(len(rows), 3)
        self.assertTrue(all(row["state"] == "待确认" for row in rows))
        self.assertNotIn("SECRET", json.dumps(rows))


    def test_cloudflare_challenge_is_explained(self):
        with patch.object(check.urllib.request, "build_opener") as factory:
            response = factory.return_value.open.return_value
            response.code = 403
            response.headers = {"cf-mitigated": "challenge"}
            rows = check.network_findings(1)
        self.assertIn("Cloudflare 人机验证", rows[0]["evidence"])
        self.assertIn("不是账号或 IP 被封", rows[0]["evidence"])


class IpTests(unittest.TestCase):
    regions = {"US": "United States of America", "JP": "Japan"}

    def detail(self, info, tz="UTC"):
        now = dt.datetime(2026, 10, 30, 12, 0, tzinfo=ZoneInfo(tz))
        return {r["name"].split("（")[0]: r for r in check.ip_detail("直连 IPv4", "203.0.113.9", info, self.regions, "2026-11-12", "https://example.test", now)}

    def test_unsupported_country_needs_action(self):
        rows = self.detail([dict(provider="ipinfo.io", country="CN", timezone="Asia/Shanghai", org="AS4134", hosting=None, proxy=None, mobile=None),
                            dict(provider="ip-api.com", country="CN", timezone="Asia/Shanghai", org="AS4134", hosting=False, proxy=False, mobile=False)],
                           tz="Asia/Shanghai")
        self.assertEqual(rows["出口 IP"]["state"], "需处理")
        self.assertIn("不在 Anthropic 支持地区名单", rows["出口 IP"]["evidence"])
        self.assertIn("换 IP 也不改变", rows["出口 IP"]["action"])
        self.assertEqual(rows["IP 类型"]["state"], "记录")
        self.assertEqual(rows["时区与 IP"]["state"], "记录")

    def test_us_datacenter_with_timezone_mismatch(self):
        rows = self.detail([dict(provider="ip-api.com", country="US", timezone="America/Los_Angeles", org="AS399804", hosting=True, proxy=False, mobile=False)])
        self.assertEqual(rows["出口 IP"]["state"], "记录")
        self.assertIn("不代表合规", rows["出口 IP"]["action"])
        self.assertEqual(rows["IP 类型"]["state"], "待确认")
        self.assertIn("机房 IP", rows["IP 类型"]["evidence"])
        self.assertIn("不代表一定会被封", rows["IP 类型"]["action"])
        self.assertEqual(rows["时区与 IP"]["state"], "待确认")
        self.assertIn("UTC−07:00", rows["时区与 IP"]["evidence"])

    def test_provider_disagreement_and_missing_data(self):
        rows = self.detail([dict(provider="ipinfo.io", country="US", timezone=None, org=None, hosting=None, proxy=None, mobile=None),
                            dict(provider="ip-api.com", country="JP", timezone=None, org=None, hosting=None, proxy=None, mobile=None)])
        self.assertEqual(rows["IP 定位分歧"]["state"], "待确认")
        self.assertEqual(self.detail([])["出口 IP"]["state"], "待确认")

    def test_full_ip_never_printed(self):
        self.assertEqual(check.mask_ip("203.0.113.9"), "203.0.113.x")
        self.assertEqual(check.mask_ip("2001:db8:10::1"), "2001:0db8:0010:…")
        rows = self.detail([dict(provider="ip-api.com", country="US", timezone="UTC", org="X", hosting=False, proxy=False, mobile=False)])
        self.assertNotIn("203.0.113.9", json.dumps(rows, ensure_ascii=False))

    def test_proxy_route_tested_only_when_configured(self):
        calls = []

        def fake(opener, url, timeout=6):
            calls.append(url)
            if "ipify" in url:
                return {"ip": "2001:db8::1" if "api6" in url else "203.0.113.9"}
            raise OSError("offline")
        with patch.object(check, "get_json", side_effect=fake):
            plain = check.ip_findings({})
            self.assertFalse(any("终端代理" in r["name"] for r in plain))
            proxied = check.ip_findings({"HTTPS_PROXY": "http://user:SECRET@127.0.0.1:9"})
        self.assertTrue(any("终端代理" in r["name"] for r in proxied))
        self.assertNotIn("SECRET", json.dumps(proxied, ensure_ascii=False))
        self.assertTrue(all("ipify" in u or "ipinfo" in u or "ip-api" in u for u in calls))

    def test_linux_language_from_localectl(self):
        with patch.object(check, "command", return_value="   System Locale: LANG=zh_CN.UTF-8\n       VC Keymap: n/a"):
            self.assertEqual(check.linux_language(), "zh_CN.UTF-8")

    def test_report_discloses_third_party_lookup(self):
        self.assertIn("会看到你的出口 IP", check.render([], disclosed=True))
        self.assertNotIn("会看到你的出口 IP", check.render([]))

    def test_regions_file_is_official_and_complete(self):
        regions, effective, source = check.load_regions()
        self.assertEqual(source, "https://www.anthropic.com/legal/supported-regions")
        self.assertGreater(len(regions), 150)
        for code in ("CN", "HK", "MO", "RU"):
            self.assertNotIn(code, regions)
        for code in ("US", "JP", "SG", "TW", "GB"):
            self.assertIn(code, regions)


if __name__ == "__main__":
    unittest.main()
