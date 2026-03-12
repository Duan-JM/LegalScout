from unittest.mock import MagicMock, patch

from law_assistant.plugins.szse_disclosure import SZSEDisclosurePlugin
from law_assistant.plugins.selectors import SZSESelectors


class TestSZSEDisclosurePlugin:
    def test_plugin_name(self):
        plugin = SZSEDisclosurePlugin()
        assert plugin.plugin_name == "szse_disclosure"

    def test_before_search_waits_for_selector(self):
        plugin = SZSEDisclosurePlugin()
        page = MagicMock()
        plugin.before_search(page)
        page.wait_for_selector.assert_called_once_with(
            SZSESelectors.SEARCH_INPUT, state="visible", timeout=15000
        )

    @patch("law_assistant.plugins.szse_disclosure.safe_click")
    @patch("law_assistant.plugins.szse_disclosure.safe_fill")
    def test_execute_search_sequence(self, mock_fill, mock_click):
        plugin = SZSEDisclosurePlugin()
        page = MagicMock()

        plugin.execute_search(page, "张三", None)

        mock_fill.assert_called_once_with(page, SZSESelectors.SEARCH_INPUT, "张三")
        mock_click.assert_called_once_with(page, SZSESelectors.SEARCH_BUTTON)
