from unittest.mock import MagicMock, patch

from law_assistant.plugins.sse_disclosure import SSEDisclosurePlugin
from law_assistant.plugins.selectors import SSESelectors


class TestSSEDisclosurePlugin:
    def test_plugin_name(self):
        plugin = SSEDisclosurePlugin()
        assert plugin.plugin_name == "sse_disclosure"

    def test_before_search_waits_for_selector(self):
        plugin = SSEDisclosurePlugin()
        page = MagicMock()
        plugin.before_search(page)
        page.wait_for_selector.assert_called_once_with(
            SSESelectors.SEARCH_INPUT, state="visible", timeout=10000
        )

    @patch("law_assistant.plugins.sse_disclosure.safe_click")
    @patch("law_assistant.plugins.sse_disclosure.safe_fill")
    def test_execute_search_sequence(self, mock_fill, mock_click):
        plugin = SSEDisclosurePlugin()
        page = MagicMock()

        plugin.execute_search(page, "张三", None)

        mock_fill.assert_called_once_with(page, SSESelectors.SEARCH_INPUT, "张三")
        assert mock_click.call_count == 3
        mock_click.assert_any_call(page, SSESelectors.SUPERVISION_BUTTON)
        mock_click.assert_any_call(page, SSESelectors.PRECISE_SEARCH_BUTTON)
        mock_click.assert_any_call(page, SSESelectors.SEARCH_BUTTON)
