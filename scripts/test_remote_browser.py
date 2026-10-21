import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("browser", Path(__file__).with_name("remote-browser.py"))
browser = importlib.util.module_from_spec(spec)
spec.loader.exec_module(browser)

class DirectoryTests(unittest.TestCase):
    @patch.object(browser.Path, "is_dir", return_value=True)
    @patch.object(browser, "capture")
    def test_shell_and_yazi(self, capture, exists):
        block = "12345678-1234-1234-1234-123456789abc"
        capture.side_effect = ["123\t/repo/shell\t/repo/yazi", "456"]
        self.assertEqual(browser.resolve_directory(block), "/repo/yazi")
        self.assertEqual(capture.call_args_list[0].args[2], "wave-12345678")
        capture.side_effect = ["123\t/repo/shell\t/repo/stale", ""]
        self.assertEqual(browser.resolve_directory(block), "/repo/shell")
    @patch.object(browser.Path, "is_dir", return_value=True)
    @patch.object(browser, "capture", return_value="")
    def test_fallback_and_invalid_id(self, capture, exists):
        self.assertEqual(browser.resolve_directory("bad;id", "/repo/fallback"), "/repo/fallback")
        capture.assert_not_called()
        self.assertEqual(browser.resolve_directory("", ""), "")
    @patch.object(browser, "capture", return_value="")
    def test_non_repo_does_not_initialize(self, capture):
        self.assertEqual(browser.repository_root("/sources"), "")
        capture.assert_called_once_with("git", "-C", "/sources", "rev-parse", "--show-toplevel")

if __name__ == "__main__":
    unittest.main()
