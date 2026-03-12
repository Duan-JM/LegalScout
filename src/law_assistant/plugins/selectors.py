"""
选择器配置文件
用于集中管理各个插件的选择器，方便维护和更新
"""


class BaseSelectors:
    """选择器基类"""

    BASE_URL: str


class CSRCSelectors(BaseSelectors):
    """证监会行政处罚选择器"""

    BASE_URL = "http://www.csrc.gov.cn/csrc/c100033/zfxxgk_zdgk.shtml#tab=gkzn"

    # 菜单导航
    MENU_LEVEL1 = "xpath=/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[1]"
    MENU_LEVEL2 = "xpath=/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[2]/div[1]/div[2]"
    MENU_ITEM = "xpath=/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[2]/div[1]/div[2]/div/ul/li[9]/a"

    # 搜索框
    SEARCH_INPUT = "xpath=/html/body/div[1]/div[3]/div[1]/div[2]/div/input[3]"
    SEARCH_BUTTON = "xpath=/html/body/div[1]/div[3]/div[1]/div[2]/div/a"

    # 结果
    RESULT_TEXT = "xpath=/html/body/div[1]/div[3]/div[3]/div[5]/div[1]/div/div/div[2]/div/div[1]/ul/table/tbody/tr[2]/td"

    # 成功关键词（表示未找到记录）
    SUCCESS_KEYWORDS = ["抱歉，没找到相关结果"]


class SSESelectors(BaseSelectors):
    """上交所信息披露选择器"""

    BASE_URL = "http://www.sse.com.cn/home/search/index.shtml"

    # 输入框
    SEARCH_INPUT = "xpath=/html/body/div[9]/div/div[1]/div/div[1]/div/div[1]/input[12]"

    # 按钮
    SUPERVISION_BUTTON = "xpath=/html/body/div[9]/div/div[1]/div/div[2]/div/div/span[5]"
    PRECISE_SEARCH_BUTTON = "xpath=/html/body/div[9]/div/div[2]/div[1]/div[6]/div[1]/div/div/div/div[1]/div/div[1]"
    SEARCH_BUTTON = "xpath=/html/body/div[9]/div/div[1]/div/div[1]/div/div[1]/input[13]"

    # 结果
    RESULT_TEXT = "xpath=/html/body/div[9]/div/div[2]/div[1]/div[6]/div[2]/ul/li"

    # 成功关键词
    SUCCESS_KEYWORDS = ["没有找到您"]


class SZSESelectors(BaseSelectors):
    """深交所信息披露选择器"""

    BASE_URL = "http://www.szse.cn/disclosure/supervision/measure/pushish/index.html"

    # 输入框（使用 ID 选择器，更稳定）
    SEARCH_INPUT = "#1800_jgxxgk_cf_tab2_txtBj"

    # 按钮
    SEARCH_BUTTON = (
        "xpath=/html/body/div[5]/div/div[2]/div/div/div[2]/div/div[7]/button"
    )

    # 结果
    RESULT_TEXT = (
        "xpath=/html/body/div[5]/div/div[2]/div/div/div[4]/div/div[2]/div/div/div[3]"
    )

    # 成功关键词
    SUCCESS_KEYWORDS = ["没有找到"]


class ShixinCSRCSelectors(BaseSelectors):
    """证券期货市场失信记录选择器"""

    BASE_URL = "https://neris.csrc.gov.cn/shixinchaxun/"

    # 输入框
    NAME_INPUT = (
        "xpath=/html/body/div/div/div/div[2]/div/div[2]/form/div[1]/div/div/input"
    )

    # 验证按钮
    VERIFY_BUTTON = "xpath=/html/body/div/div/div/div[2]/div/div[2]/form/div[3]/div/div"

    # 滑块验证
    CAPTCHA_IMAGE = "xpath=/html/body/div/div/div/div[3]/div/div[2]/div/div[1]/div/img"
    SLIDER_BUTTON = "xpath=/html/body/div/div/div/div[3]/div/div[2]/div/div[2]/div/div"

    # 结果
    RESULT_TEXT = "xpath=/html/body/div/div/div/div[4]/div[2]/ul/li/div[2]"

    # 成功关键词
    SUCCESS_KEYWORDS = ["无符合条件记录"]

    # 配置
    MANUAL_OFFSET = 20  # 滑块偏移量
    MAX_SLIP_FAILED_CNT = 5  # 最大滑块尝试次数


# 导出所有选择器
__all__ = ["CSRCSelectors", "SSESelectors", "SZSESelectors", "ShixinCSRCSelectors"]
