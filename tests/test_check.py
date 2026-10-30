import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

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
        self.assertIn("剩几天退几天", check.render([]))

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


if __name__ == "__main__":
    unittest.main()
