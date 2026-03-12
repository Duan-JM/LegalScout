from unittest.mock import MagicMock, call, patch

from law_assistant.plugins.csrc import CSRCPlugin
from law_assistant.plugins.selectors import CSRCSelectors


class TestCSRCPlugin:
    def test_plugin_name(self):
        plugin = CSRCPlugin()
        assert plugin.plugin_name == "csrc"

    def test_selectors_type(self):
        plugin = CSRCPlugin()
        assert plugin.selectors is CSRCSelectors

    @patch("law_assistant.plugins.csrc.safe_click")
    @patch("law_assistant.plugins.csrc.safe_fill")
    def test_execute_search_calls_correct_sequence(self, mock_fill, mock_click):
        plugin = CSRCPlugin()
        page = MagicMock()

        plugin.execute_search(page, "张三", None)

        assert mock_click.call_count == 4
        mock_click.assert_any_call(page, CSRCSelectors.MENU_LEVEL1)
        mock_click.assert_any_call(page, CSRCSelectors.MENU_LEVEL2)
        mock_click.assert_any_call(page, CSRCSelectors.MENU_ITEM)
        mock_click.assert_any_call(page, CSRCSelectors.SEARCH_BUTTON)

        mock_fill.assert_called_once_with(page, CSRCSelectors.SEARCH_INPUT, "张三")

        # Verify order: click menu1, click menu2, click item, fill, click search
        expected_calls = [
            call(page, CSRCSelectors.MENU_LEVEL1),
            call(page, CSRCSelectors.MENU_LEVEL2),
            call(page, CSRCSelectors.MENU_ITEM),
        ]
        assert mock_click.call_args_list[:3] == expected_calls
        assert mock_click.call_args_list[3] == call(page, CSRCSelectors.SEARCH_BUTTON)
