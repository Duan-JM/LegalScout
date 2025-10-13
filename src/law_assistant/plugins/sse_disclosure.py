"""
上交所信息披露查询插件
"""

from playwright.sync_api import Page

from law_assistant.plugins.base import SimpleSearchPlugin
from law_assistant.plugins.selectors import SSESelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class SSEDisclosurePlugin(SimpleSearchPlugin):
    """上交所信息披露查询插件"""

    @property
    def plugin_name(self) -> str:
        return "上交所信息披露"

    @property
    def selectors(self):
        return SSESelectors

    def before_search(self, page: Page) -> None:
        """页面加载后等待"""
        page.wait_for_timeout(3000)

    def execute_search(self, page: Page, name: str, _) -> None:
        safe_fill(page, SSESelectors.SEARCH_INPUT, name)
        safe_click(page, SSESelectors.SUPERVISION_BUTTON)
        safe_click(page, SSESelectors.PRECISE_SEARCH_BUTTON)
        safe_click(page, SSESelectors.SEARCH_BUTTON)


_plugin_instance = SSEDisclosurePlugin()


def find_evidence_func(name: str, output_dir: str, dev: bool = False):
    _plugin_instance.find_evidence_func(name, output_dir, dev)


def api_v1(input_file: str, output_dir: str, process_num: int = 10, dev: bool = False):
    """插件入口函数（向后兼容函数）"""
    _plugin_instance.api_v1(input_file, output_dir, process_num, dev)
