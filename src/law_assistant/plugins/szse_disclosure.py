from playwright.sync_api import Page

from law_assistant.plugins.base import SimpleSearchPlugin
from law_assistant.plugins.selectors import SZSESelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class SZSEDisclosurePlugin(SimpleSearchPlugin):
    """深交所信息披露查询插件"""

    @property
    def plugin_name(self) -> str:
        return "szse_disclosure"

    @property
    def selectors(self):
        return SZSESelectors

    def before_search(self, page: Page) -> None:
        """页面加载后等待一段时间"""

        page.wait_for_timeout(6000)

    def execute_search(self, page: Page, name: str) -> None:
        safe_fill(page, SZSESelectors.SEARCH_INPUT, name)
        safe_click(page, SZSESelectors.SEARCH_BUTTON)


_plugin_instance = SZSEDisclosurePlugin()


def find_evidence_func(name: str, output_dir: str, dev: bool = False):
    """深交所信息披露查询（向后兼容函数）"""
    _plugin_instance.find_evidence_func(name, output_dir, dev)


def api_v1(input_file: str, output_dir: str, process_num: int = 10, dev: bool = False):
    _plugin_instance.api_v1(input_file, output_dir, process_num, dev)
