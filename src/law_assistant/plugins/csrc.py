"""
证监会行政处罚查询插件

"""

from playwright.sync_api import Page

from law_assistant.plugins.base import SimpleSearchPlugin
from law_assistant.plugins.selectors import CSRCSelectors
from law_assistant.plugins.utils import safe_click, safe_fill


class CSRCPlugin(SimpleSearchPlugin):
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


_plugin_instance = CSRCPlugin()


def find_evidence_func(name: str, output_dir: str, dev: bool = False):
    """证监会行政处罚查询（向后兼容函数）"""
    _plugin_instance.find_evidence_func(name, output_dir, dev)


def api_v1(input_file: str, output_dir: str, process_num: int = 10, dev: bool = False):
    """插件入口函数（向后兼容函数）"""
    _plugin_instance.api_v1(input_file, output_dir, process_num, dev)
