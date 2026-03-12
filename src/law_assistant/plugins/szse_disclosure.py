"""
深交所信息披露查询插件
"""

from playwright.sync_api import Page

from law_assistant.plugins.base import BasePlugin
from law_assistant.plugins.selectors import SZSESelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class SZSEDisclosurePlugin(BasePlugin):
    """深交所信息披露查询插件"""

    @property
    def plugin_name(self) -> str:
        return "szse_disclosure"

    @property
    def selectors(self):
        return SZSESelectors

    def before_search(self, page: Page) -> None:
        """等待页面搜索框加载完毕"""
        page.wait_for_selector(SZSESelectors.SEARCH_INPUT, state="visible", timeout=15000)

    def execute_search(self, page: Page, name: str, _) -> None:
        safe_fill(page, SZSESelectors.SEARCH_INPUT, name)
        safe_click(page, SZSESelectors.SEARCH_BUTTON)
