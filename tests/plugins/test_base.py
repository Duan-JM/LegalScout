from unittest.mock import MagicMock, patch, PropertyMock

import pytest
from playwright.sync_api import Page

from law_assistant.plugins.base import BasePlugin
from law_assistant.plugins.selectors import CSRCSelectors


class ConcretePlugin(BasePlugin):
    """Test stub implementing all abstract methods."""

    @property
    def plugin_name(self) -> str:
        return "test_plugin"

    @property
    def selectors(self):
        return CSRCSelectors

    def execute_search(self, page: Page, name: str, context) -> None:
        pass


class ConcretePluginWithCustomError(ConcretePlugin):
    """Test stub with custom error handling."""

    def handle_search_error(self, page, error):
        return "自定义错误"


# ==================== Properties ====================


class TestBasePluginProperties:
    def test_default_watermark_position(self):
        plugin = ConcretePlugin()
        assert plugin.watermark_position == (40, 60)

    def test_default_watermark_color(self):
        plugin = ConcretePlugin()
        assert plugin.watermark_color == "black"

    def test_base_url_from_selectors(self):
        plugin = ConcretePlugin()
        assert plugin.base_url == CSRCSelectors.BASE_URL

    def test_default_page_load_timeout(self):
        plugin = ConcretePlugin()
        assert plugin.page_load_timeout == 60000


# ==================== _generate_filename ====================


class TestGenerateFilename:
    def test_success_returns_name(self):
        plugin = ConcretePlugin()
        assert plugin._generate_filename("张三", True) == "张三"

    def test_failure_appends_suffix(self):
        plugin = ConcretePlugin()
        assert plugin._generate_filename("张三", False) == "张三 - 异常"


# ==================== _handle_error ====================


class TestHandleError:
    def test_default_suffix(self, mock_page, tmp_output_dir):
        plugin = ConcretePlugin()
        with patch.object(plugin, "_save_screenshot") as mock_save:
            plugin._handle_error(mock_page, "张三", RuntimeError("boom"), tmp_output_dir)
            mock_save.assert_called_once_with(
                mock_page, "张三 - 系统异常", tmp_output_dir
            )

    def test_custom_suffix(self, mock_page, tmp_output_dir):
        plugin = ConcretePluginWithCustomError()
        with patch.object(plugin, "_save_screenshot") as mock_save:
            plugin._handle_error(mock_page, "张三", RuntimeError("boom"), tmp_output_dir)
            mock_save.assert_called_once_with(
                mock_page, "张三 - 自定义错误", tmp_output_dir
            )

    def test_screenshot_failure_logged(self, mock_page, tmp_output_dir):
        plugin = ConcretePlugin()
        with patch.object(
            plugin, "_save_screenshot", side_effect=Exception("screenshot failed")
        ):
            # Should not raise, just log the error
            plugin._handle_error(mock_page, "张三", RuntimeError("boom"), tmp_output_dir)


# ==================== check_result ====================


class TestCheckResult:
    def test_returns_true_when_no_records(self, mock_page):
        plugin = ConcretePlugin()
        with patch(
            "law_assistant.plugins.base.check_no_records_found",
            return_value=(True, False),
        ):
            assert plugin.check_result(mock_page) is True

    def test_returns_false_when_records_found(self, mock_page):
        plugin = ConcretePlugin()
        with patch(
            "law_assistant.plugins.base.check_no_records_found",
            return_value=(False, False),
        ):
            assert plugin.check_result(mock_page) is False


# ==================== find_evidence_func ====================


class TestFindEvidenceFunc:
    @patch("law_assistant.plugins.base.sync_playwright")
    @patch("law_assistant.plugins.base.get_browser_and_page")
    @patch("law_assistant.plugins.base.capture_screenshot")
    @patch("law_assistant.plugins.base._save_processed_name")
    @patch("law_assistant.plugins.base._get_progress_file", return_value="/tmp/progress.json")
    @patch("law_assistant.plugins.base.check_no_records_found", return_value=(True, False))
    def test_full_workflow_success(
        self,
        mock_check,
        mock_progress_file,
        mock_save_name,
        mock_capture,
        mock_get_browser,
        mock_playwright,
    ):
        plugin = ConcretePlugin()
        mock_page = MagicMock()
        mock_browser = MagicMock()
        mock_context = MagicMock()
        mock_get_browser.return_value = (mock_browser, mock_page, mock_context)

        # Setup sync_playwright context manager
        mock_pw = MagicMock()
        mock_playwright.return_value.__enter__ = MagicMock(return_value=mock_pw)
        mock_playwright.return_value.__exit__ = MagicMock(return_value=False)

        plugin.find_evidence_func("张三", "/tmp/output")

        mock_page.goto.assert_called_once()
        mock_browser.close.assert_called_once()
        mock_capture.assert_called_once()

    @patch("law_assistant.plugins.base.sync_playwright")
    @patch("law_assistant.plugins.base.get_browser_and_page")
    @patch("law_assistant.plugins.base.capture_screenshot")
    def test_workflow_handles_exception(
        self, mock_capture, mock_get_browser, mock_playwright
    ):
        plugin = ConcretePlugin()
        mock_page = MagicMock()
        mock_browser = MagicMock()
        mock_context = MagicMock()
        mock_get_browser.return_value = (mock_browser, mock_page, mock_context)

        mock_pw = MagicMock()
        mock_playwright.return_value.__enter__ = MagicMock(return_value=mock_pw)
        mock_playwright.return_value.__exit__ = MagicMock(return_value=False)

        # Make goto raise an exception
        mock_page.goto.side_effect = RuntimeError("navigation failed")

        plugin.find_evidence_func("张三", "/tmp/output")

        # Error screenshot should be attempted
        mock_capture.assert_called_once()
        mock_browser.close.assert_called_once()

    @patch("law_assistant.plugins.base.sync_playwright")
    @patch("law_assistant.plugins.base.get_browser_and_page")
    def test_browser_always_closed(self, mock_get_browser, mock_playwright):
        plugin = ConcretePlugin()
        mock_page = MagicMock()
        mock_browser = MagicMock()
        mock_context = MagicMock()
        mock_get_browser.return_value = (mock_browser, mock_page, mock_context)

        mock_pw = MagicMock()
        mock_playwright.return_value.__enter__ = MagicMock(return_value=mock_pw)
        mock_playwright.return_value.__exit__ = MagicMock(return_value=False)

        mock_page.goto.side_effect = RuntimeError("crash")

        plugin.find_evidence_func("张三", "/tmp/output")

        mock_browser.close.assert_called_once()
