"""
证监会行政处罚查询插件
"""

from playwright.sync_api import Page

from law_assistant.plugins.base import BasePlugin
from law_assistant.plugins.selectors import CSRCSelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class CSRCPlugin(BasePlugin):
    """证监会行政处罚查询插件"""

    @property
    def plugin_name(self) -> str:
        return "csrc"

    @property
    def selectors(self):
        return CSRCSelectors

    def execute_search(self, page: Page, name: str, _) -> None:
        """执行搜索操作"""
        safe_click(page, CSRCSelectors.MENU_LEVEL1)
        safe_click(page, CSRCSelectors.MENU_LEVEL2)
        safe_click(page, CSRCSelectors.MENU_ITEM)
        safe_fill(page, CSRCSelectors.SEARCH_INPUT, name)
        safe_click(page, CSRCSelectors.SEARCH_BUTTON)
