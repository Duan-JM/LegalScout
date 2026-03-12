"""
上交所信息披露查询插件
"""

from playwright.sync_api import Page

from law_assistant.plugins.base import BasePlugin
from law_assistant.plugins.selectors import SSESelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class SSEDisclosurePlugin(BasePlugin):
    """上交所信息披露查询插件"""

    @property
    def plugin_name(self) -> str:
        return "sse_disclosure"

    @property
    def selectors(self):
        return SSESelectors

    def before_search(self, page: Page) -> None:
        """等待页面搜索框加载完毕"""
        page.wait_for_selector(SSESelectors.SEARCH_INPUT, state="visible", timeout=10000)

    def execute_search(self, page: Page, name: str, _) -> None:
        safe_fill(page, SSESelectors.SEARCH_INPUT, name)
        safe_click(page, SSESelectors.SUPERVISION_BUTTON)
        safe_click(page, SSESelectors.PRECISE_SEARCH_BUTTON)
        safe_click(page, SSESelectors.SEARCH_BUTTON)
