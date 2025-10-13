import base64
import io
import os
import time
from datetime import datetime
from functools import wraps
from typing import Callable, Optional, Tuple

import structlog
from PIL import Image, ImageDraw, ImageFont
from playwright.sync_api import Page, Playwright
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

logger = structlog.getLogger(__name__)


def get_browser_and_page(p: Playwright, dev=False):
    """
    Get browser and page from Playwright instance.
    Connects to remote Browserless Chrome via CDP.

    Args:
        p: Playwright instance from sync_playwright() context

    Returns:
        tuple: (browser, page) - Browser and Page instances
    """
    logger.info(f"Launching browser, dev mode: {dev}")
    browserless_url = os.getenv("BROWSERLESS_URL", "http://localhost:3000")
    if dev:
        # 开发者模式使用本地 chromium 打开
        browser = p.chromium.launch(
            headless=False,  # 关键：False 表示显示浏览器窗口
            slow_mo=500,  # 可选：每个操作延迟 500ms，方便观察
        )
    else:
        browser = p.chromium.connect_over_cdp(browserless_url)
    context = browser.contexts[0] if browser.contexts else browser.new_context()
    page = context.new_page()
    return browser, page, context


def retry_on_failure(max_retries: int = 3, delay: int = 2, backoff: float = 1.5):
    """
    重试装饰器

    Args:
        max_retries: 最大重试次数
        delay: 初始延迟时间（秒）
        backoff: 退避因子，每次重试延迟时间乘以此因子

    Examples:
        @retry_on_failure(max_retries=3, delay=2)
        def unstable_function():
            # 可能失败的操作
            pass
    """

    def decorator(func: Callable) -> Callable:
        @wraps(func)
        def wrapper(*args, **kwargs):
            current_delay = delay
            for attempt in range(max_retries):
                try:
                    return func(*args, **kwargs)
                except Exception as e:
                    if attempt == max_retries - 1:
                        logger.error(
                            f"Function {func.__name__} failed after {max_retries} attempts",
                            error=str(e),
                            error_type=type(e).__name__,
                        )
                        raise
                    logger.warning(
                        f"Attempt {attempt + 1}/{max_retries} failed for {func.__name__}: {e}, "
                        f"retrying in {current_delay}s..."
                    )
                    time.sleep(current_delay)
                    current_delay *= backoff
            return None

        return wrapper

    return decorator


def safe_click(
    page: Page, selector: str, timeout: int = 10000, retries: int = 2
) -> bool:
    """
    安全地点击元素，带重试机制

    Args:
        page: Playwright Page 对象
        selector: 元素选择器
        timeout: 超时时间（毫秒）
        retries: 重试次数

    Returns:
        bool: 点击是否成功
    """
    for attempt in range(retries):
        try:
            page.click(selector, timeout=timeout)
            logger.debug(f"Successfully clicked: {selector}")
            return True
        except PlaywrightTimeoutError:
            if attempt == retries - 1:
                logger.error(
                    f"Failed to click element after {retries} attempts: {selector}"
                )
                return False
            logger.warning(
                f"Click attempt {attempt + 1} failed for {selector}, retrying..."
            )
            page.wait_for_timeout(1000)
        except Exception as e:
            logger.error(f"Unexpected error clicking {selector}: {e}")
            return False
    return False


def safe_fill(page: Page, selector: str, text: str, timeout: int = 10000) -> bool:
    """
    安全地填充文本

    Args:
        page: Playwright Page 对象
        selector: 元素选择器
        text: 要填充的文本
        timeout: 超时时间（毫秒）

    Returns:
        bool: 填充是否成功
    """
    try:
        page.fill(selector, text, timeout=timeout)
        logger.debug(f"Successfully filled text into: {selector}")
        return True
    except PlaywrightTimeoutError:
        logger.error(f"Timeout filling text into: {selector}")
        return False
    except Exception as e:
        logger.error(f"Error filling text into {selector}: {e}")
        return False


def safe_get_text(page: Page, selector: str, timeout: int = 10000) -> Optional[str]:
    """
    安全地获取元素文本内容

    Args:
        page: Playwright Page 对象
        selector: 元素选择器
        timeout: 超时时间（毫秒）

    Returns:
        Optional[str]: 元素文本内容，失败返回 None
    """
    try:
        locator = page.locator(selector)
        locator.wait_for(timeout=timeout, state="visible")
        text = locator.text_content()
        logger.debug(f"Successfully got text from: {selector}")
        return text
    except PlaywrightTimeoutError:
        logger.warning(f"Timeout getting text from: {selector}")
        return None
    except Exception as e:
        logger.error(f"Error getting text from {selector}: {e}")
        return None


def check_search_result(
    page: Page, selector: str, success_keywords: list, timeout: int = 10000
) -> Tuple[bool, bool]:
    """
    检查搜索结果

    Args:
        page: Playwright Page 对象
        selector: 结果元素选择器
        success_keywords: 表示"未找到"的关键词列表（如：["没找到", "无记录"]）
        timeout: 超时时间（毫秒）

    Returns:
        Tuple[bool, bool]: (find_normal_flag, system_error_flag)
            - find_normal_flag: True 表示正常（未找到记录）
            - system_error_flag: True 表示系统错误
    """
    text = safe_get_text(page, selector, timeout)

    if text is None:
        # 无法获取文本，可能是系统错误
        return False, True

    # 检查是否包含成功关键词
    find_normal = any(keyword in text for keyword in success_keywords)
    return find_normal, False


def fetch_names(input_file):
    with open(input_file, "r") as f:
        names = f.readlines()
    return [x.strip() for x in names]


def generate_names(input_names, output_dir, plugin_name):
    target_path = f"{output_dir}/{plugin_name}"
    if os.path.exists(target_path):
        exist_names = os.listdir(target_path)
        logger.info(f"Found exist {len(exist_names)} pdf")
        normal_names = [
            ".".join(name.split(".")[:-1]) for name in exist_names if "异常" not in name
        ]
        abnormal_names = [
            name.split("-")[0].strip() for name in exist_names if "异常" in name
        ]
        exist_names = normal_names + abnormal_names
        need_fetch_names = list(set(input_names) - set(exist_names))
    else:
        os.mkdir(target_path)
        logger.warning(f"No exist result found. Makedir {target_path}!")
        need_fetch_names = input_names
    return need_fetch_names


def watermark(
    image_bytes, watermark_text: str, position: Tuple, filled_color: str = "black"
):
    font = ImageFont.truetype("STHeiti Light.ttc", 33, encoding="unic")
    image = Image.open(io.BytesIO(image_bytes))
    width, height = image.size
    assert width > position[0] and height > position[1]
    drawing = ImageDraw.Draw(image)
    drawing.text(xy=position, text=watermark_text, fill=filled_color, font=font)
    buffered = io.BytesIO()
    image.save(buffered, format="PNG")
    return base64.b64encode(buffered.getvalue())


def capture_screenshot(
    page: Page,
    plugin_name: str,
    file_name: str,
    output_dir: str,
    position: Tuple,
    filled_color: str,
):
    """Capture screenshot using Playwright Page object"""
    # Take full page screenshot
    screenshot_bytes = page.screenshot(full_page=True)

    timestamp = datetime.now().strftime("%Y-%m-%d")
    pdf_data = watermark(
        image_bytes=screenshot_bytes,
        watermark_text=f"{timestamp} - {file_name}",
        position=position,
        filled_color=filled_color,
    )
    with open(f"{output_dir}/{plugin_name}/{file_name}.png", "wb") as file:
        file.write(base64.b64decode(pdf_data))
        file.write(base64.b64decode(pdf_data))
