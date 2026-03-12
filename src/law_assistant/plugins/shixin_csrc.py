"""证券期货市场失信记录查询插件"""

import base64
from typing import Optional, Tuple

import cv2
import numpy as np
import structlog
from playwright.sync_api import Page

from law_assistant.plugins.base import BasePlugin
from law_assistant.plugins.selectors import ShixinCSRCSelectors
from law_assistant.plugins.utils import safe_click, safe_fill

logger = structlog.getLogger(__name__)


class ShixinCSRCPlugin(BasePlugin):
    """证券期货市场失信记录查询插件（带滑块验证码）"""

    @property
    def plugin_name(self) -> str:
        return "shixin_csrc"

    @property
    def watermark_position(self) -> Tuple[int, int]:
        return (60, 120)

    @property
    def selectors(self):
        return ShixinCSRCSelectors

    def _find_slide_position(self, background_img: np.ndarray) -> Tuple[int, int]:
        """
        使用边界框检测滑块目标位置

        Args:
            background_img: 背景图片的 NumPy 数组

        Returns:
            Tuple[int, int]: (滑块移动距离, Logo宽度)
        """
        image = cv2.cvtColor(background_img, cv2.COLOR_BGR2RGB)
        canny = cv2.Canny(image, 500, 700)

        contours, _ = cv2.findContours(canny, cv2.RETR_CCOMP, cv2.CHAIN_APPROX_SIMPLE)
        dx, width = 0, 0
        for contour in contours:
            x, y, w, h = cv2.boundingRect(contour)
            if (w > 30) and (h > 30):
                dx = x
                width = w
                cv2.rectangle(image, (x, y), (x + w, y + h), (0, 0, 255), 2)
        return dx, width

    def _verify_slide_captcha(self, page: Page) -> None:
        """
        验证滑块验证码

        通过 OpenCV 图像处理识别滑块位置，然后模拟鼠标拖动操作
        """
        page.wait_for_selector(
            self.selectors.CAPTCHA_IMAGE, state="visible", timeout=5000
        )

        # 获取验证码图片
        background_img = page.locator(self.selectors.CAPTCHA_IMAGE)
        raw_image_data = background_img.get_attribute("src").split(",")[1]
        nparr = np.frombuffer(base64.b64decode(raw_image_data), np.uint8)
        img = cv2.imdecode(nparr, cv2.IMREAD_COLOR)

        # 计算滑块位置
        move_dx, logo_width = self._find_slide_position(img)

        # 获取滑块按钮
        btn = page.locator(self.selectors.SLIDER_BUTTON)
        box = btn.bounding_box()

        if box:
            target_x = box["x"] + move_dx + logo_width + self.selectors.MANUAL_OFFSET

            # 模拟人工拖动
            page.mouse.move(box["x"] + box["width"] / 2, box["y"] + box["height"] / 2)
            page.mouse.down()
            page.mouse.move(target_x, box["y"] + box["height"] / 2, steps=10)
            page.mouse.up()

        page.wait_for_timeout(3000)

    def handle_search_error(self, page: Page, error: Exception) -> Optional[str]:
        """自定义错误信息"""
        error_str = str(error).lower()
        if "captcha" in error_str or "验证" in error_str:
            return "验证码验证失败"
        return None

    def execute_search(self, page: Page, name: str, context) -> None:
        """执行搜索操作"""

        safe_fill(page, self.selectors.NAME_INPUT, name)
        safe_click(page, ShixinCSRCSelectors.VERIFY_BUTTON)
        failed_cnt = 0
        initial_page_count = len(context.pages)
        while (
            len(context.pages) == initial_page_count
            and failed_cnt < self.selectors.MAX_SLIP_FAILED_CNT
        ):
            try:
                self._verify_slide_captcha(page)
                failed_cnt += 1
            except Exception as e:
                logger.warning(
                    f"[{self.plugin_name}] Captcha verification attempt "
                    f"{failed_cnt} failed: {str(e)}"
                )
                failed_cnt += 1

        if len(context.pages) > initial_page_count:
            page = context.pages[-1]
            logger.info(
                f"[{self.plugin_name}] Captcha verified successfully for {name}"
            )

        else:
            logger.error(
                f"[{self.plugin_name}] Captcha verification failed after "
                f"{failed_cnt} attempts for {name}"
            )
            raise RuntimeError("验证码验证失败")
