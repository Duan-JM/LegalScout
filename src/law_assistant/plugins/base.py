"""
插件基类模块

定义了所有法律查询插件的通用接口和流程，使用模板方法模式减少重复代码
"""

from abc import ABC, abstractmethod
from functools import partial
from multiprocessing import Pool
from typing import Optional, Tuple

import structlog
from playwright.sync_api import Page, sync_playwright
from tqdm import tqdm

from law_assistant.plugins.utils import (
    capture_screenshot,
    check_search_result,
    fetch_names,
    generate_names,
    get_browser_and_page,
)

logger = structlog.getLogger(__name__)


class BasePlugin(ABC):
    """
    插件基类，定义了所有法律查询插件的通用接口和流程

    子类只需实现核心的搜索逻辑，其他流程由基类统一处理

    使用模板方法模式：
    - find_evidence_func() 提供完整的查询流程框架
    - execute_search() 由子类实现具体的搜索逻辑
    - check_result() 由子类实现结果检查逻辑
    """

    # ========== 类属性（子类必须定义）==========

    @property
    @abstractmethod
    def plugin_name(self) -> str:
        """插件名称，用于日志和文件命名"""
        pass

    @property
    def watermark_position(self) -> Tuple[int, int]:
        """截图水印位置，默认 (40, 60)"""
        return (40, 60)

    @property
    def watermark_color(self) -> str:
        """截图水印颜色，默认 black"""
        return "black"

    @property
    @abstractmethod
    def selectors(self):
        """
        选择器配置对象（从 selectors.py 导入）

        例如：
            return CSRCSelectors
        """
        pass

    @property
    @abstractmethod
    def base_url(self) -> str:
        """目标网站 URL"""
        pass

    @property
    def page_load_timeout(self) -> int:
        """页面加载超时时间（毫秒），默认 60 秒"""
        return 60000

    # ========== 抽象方法（子类必须实现）==========
    @abstractmethod
    def execute_search(self, page: Page, name: str, context) -> None:
        """
        执行搜索的核心逻辑

        子类实现具体的搜索步骤，如：
        - 填写表单
        - 点击按钮
        - 处理验证码等

        Args:
            page: Playwright 页面对象
            name: 查询的名称
        """
        pass

    # ========== 钩子方法（子类可选重写）==========

    def before_search(self, page: Page) -> None:
        """
        搜索前的准备工作（可选）

        例如：等待特定元素加载、设置 cookies 等
        """
        pass

    def after_search(self, page: Page) -> None:
        """
        搜索后的清理工作（可选）

        例如：关闭弹窗、清理缓存等
        """
        pass

    def handle_search_error(self, page: Page, error: Exception) -> Optional[str]:
        """
        自定义错误处理（可选）

        Returns:
            自定义的错误文件名后缀，None 则使用默认 "系统异常"
        """
        return None

    # ========== 模板方法（提供完整流程）==========

    def check_result(self, page: Page) -> bool:
        """
        统一的结果检查逻辑

        使用 check_search_result 工具函数检查结果
        """
        return check_search_result(
            page, self.selectors.RESULT_TEXT, self.selectors.SUCCESS_KEYWORDS
        )

    def find_evidence_func(self, name: str, output_dir: str, dev: bool = False) -> None:
        """
        查询证据的模板方法

        定义了完整的查询流程，子类无需重写：
        1. 创建浏览器实例
        2. 导航到目标页面
        3. 执行搜索（调用子类实现）
        4. 检查结果（调用子类实现）
        5. 保存截图
        6. 异常处理
        7. 资源清理

        Args:
            name: 查询的名称
            output_dir: 输出目录
            dev: 开发模式（是否显示浏览器界面）
        """
        with sync_playwright() as p:
            browser, page, context = get_browser_and_page(p, dev=dev)

            try:
                # Step 1: 导航到目标页面
                logger.info(
                    f"[{self.plugin_name}] Navigating to {self.base_url} for {name}"
                )
                page.goto(self.base_url, timeout=self.page_load_timeout)

                # Step 2: 搜索前准备（钩子）
                self.before_search(page)

                # Step 3: 执行搜索（核心逻辑，子类实现）
                logger.info(f"[{self.plugin_name}] Executing search for {name}")
                self.execute_search(page, name, context)

                # Step 4: 检查搜索结果（子类实现）
                is_success = self.check_result(page)

                # Step 5: 生成文件名
                file_name = self._generate_filename(name, is_success)

                # Step 6: 搜索后清理（钩子）
                self.after_search(page)

                # Step 7: 保存截图
                self._save_screenshot(page, file_name, output_dir)

                logger.info(f"[{self.plugin_name}] Successfully processed {name}")

            except Exception as e:
                # Step 8: 异常处理
                self._handle_error(page, name, e, output_dir)

            finally:
                # Step 9: 资源清理
                browser.close()

    def api_v1(
        self, input_file: str, output_dir: str, process_num: int = 10, dev: bool = False
    ) -> None:
        """
        插件的统一入口函数

        所有插件共享相同的实现，无需在子类中重写

        功能：
        - 读取输入文件中的名称列表
        - 过滤已处理的名称
        - 使用多进程并发处理
        - 显示进度条

        Args:
            input_file: 输入文件路径
            output_dir: 输出目录路径
            process_num: 并发进程数
            dev: 开发模式（是否显示浏览器界面）
        """
        require_names = fetch_names(input_file)
        names = generate_names(
            input_names=require_names,
            output_dir=output_dir,
            plugin_name=self.plugin_name,
        )

        logger.info(
            f"[{self.plugin_name}] Processing {len(names)} names "
            f"with {process_num} processes"
        )

        pbar = tqdm(total=len(names), desc=self.plugin_name)
        with Pool(processes=process_num) as pool:
            for _ in pool.imap_unordered(
                partial(self.find_evidence_func, output_dir=output_dir, dev=dev), names
            ):
                pbar.update(1)

        pbar.close()
        logger.info(f"[{self.plugin_name}] Completed processing all names")

    # ========== 私有辅助方法 ==========

    def _generate_filename(self, name: str, is_success: bool) -> str:
        """
        生成截图文件名

        Args:
            name: 查询的名称
            is_success: 是否为正常结果（无记录）

        Returns:
            文件名字符串
        """
        if is_success:
            logger.info(f"[{self.plugin_name}] No records found for {name}")
            return name
        else:
            logger.warning(f"[{self.plugin_name}] Found abnormal records for {name}")
            return name + " - 异常"

    def _save_screenshot(self, page: Page, file_name: str, output_dir: str) -> None:
        """
        保存截图

        Args:
            page: Playwright 页面对象
            file_name: 文件名
            output_dir: 输出目录
        """
        capture_screenshot(
            page=page,
            plugin_name=self.plugin_name,
            file_name=file_name,
            output_dir=output_dir,
            position=self.watermark_position,
            filled_color=self.watermark_color,
        )

    def _handle_error(
        self, page: Page, name: str, error: Exception, output_dir: str
    ) -> None:
        """
        统一的错误处理

        Args:
            page: Playwright 页面对象
            name: 查询的名称
            error: 捕获的异常
            output_dir: 输出目录
        """
        # 尝试自定义错误处理
        custom_suffix = self.handle_search_error(page, error)
        suffix = custom_suffix if custom_suffix else "系统异常"

        file_name = f"{name} - {suffix}"
        logger.error(
            f"[{self.plugin_name}] Error while processing {name}: {str(error)}",
            exc_info=True,
        )

        # 尝试保存错误截图
        try:
            self._save_screenshot(page, file_name, output_dir)
        except Exception as screenshot_error:
            logger.error(
                f"[{self.plugin_name}] Failed to capture error screenshot "
                f"for {name}: {screenshot_error}"
            )


class SimpleSearchPlugin(BasePlugin):
    """
    简单搜索插件的中间层

    适用于大多数标准搜索场景（csrc、sse、szse）
    提供了基于选择器配置的通用实现

    子类只需：
    1. 定义 plugin_name、base_url、selectors
    2. 实现 execute_search() 方法
    """

    @property
    @abstractmethod
    def selectors(self):
        """
        选择器配置对象（从 selectors.py 导入）

        例如：
            return CSRCSelectors
        """
        pass

    @property
    def base_url(self) -> str:
        """从选择器配置中获取 base_url"""
        return self.selectors.BASE_URL
