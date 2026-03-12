import io
import json
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
) -> None:
    """
    安全地点击元素，带重试机制，失败时抛出异常

    Args:
        page: Playwright Page 对象
        selector: 元素选择器
        timeout: 超时时间（毫秒）
        retries: 重试次数

    Raises:
        RuntimeError: 所有重试均失败
    """
    for attempt in range(retries):
        try:
            page.click(selector, timeout=timeout)
            logger.debug(f"Successfully clicked: {selector}")
            return
        except PlaywrightTimeoutError:
            if attempt == retries - 1:
                raise RuntimeError(
                    f"Failed to click element after {retries} attempts: {selector}"
                )
            logger.warning(
                f"Click attempt {attempt + 1} failed for {selector}, retrying..."
            )
            page.wait_for_timeout(1000)


def safe_fill(page: Page, selector: str, text: str, timeout: int = 10000) -> None:
    """
    安全地填充文本，失败时抛出异常

    Args:
        page: Playwright Page 对象
        selector: 元素选择器
        text: 要填充的文本
        timeout: 超时时间（毫秒）

    Raises:
        RuntimeError: 填充失败
    """
    try:
        page.fill(selector, text, timeout=timeout)
        logger.debug(f"Successfully filled text into: {selector}")
    except PlaywrightTimeoutError:
        raise RuntimeError(f"Timeout filling text into: {selector}")
    except Exception as e:
        raise RuntimeError(f"Error filling text into {selector}: {e}")


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


def check_no_records_found(
    page: Page, selector: str, no_record_keywords: list, timeout: int = 10000
) -> Tuple[bool, bool]:
    """
    检查搜索结果是否为"无记录"状态

    Args:
        page: Playwright Page 对象
        selector: 结果元素选择器
        no_record_keywords: 表示"未找到记录"的关键词列表（如：["没找到", "无记录"]）
        timeout: 超时时间（毫秒）

    Returns:
        Tuple[bool, bool]: (no_records_found, is_system_error)
            - no_records_found: True 表示正常（未找到违规记录）
            - is_system_error: True 表示系统错误
    """
    text = safe_get_text(page, selector, timeout)

    if text is None:
        return False, True

    no_records = any(keyword in text for keyword in no_record_keywords)
    return no_records, False


def fetch_names(input_file):
    with open(input_file, "r") as f:
        names = f.readlines()
    return [x.strip() for x in names]


def _get_progress_file(output_dir: str, plugin_name: str) -> str:
    """获取进度记录文件路径"""
    return os.path.join(output_dir, plugin_name, ".processed.json")


def _load_processed_names(progress_file: str) -> set:
    """加载已处理的名称集合"""
    if os.path.exists(progress_file):
        with open(progress_file, "r", encoding="utf-8") as f:
            return set(json.load(f))
    return set()


def _save_processed_name(progress_file: str, name: str) -> None:
    """追加记录一个已处理的名称"""
    processed = _load_processed_names(progress_file)
    processed.add(name)
    with open(progress_file, "w", encoding="utf-8") as f:
        json.dump(sorted(processed), f, ensure_ascii=False, indent=2)


def generate_names(input_names, output_dir, plugin_name):
    """
    过滤已处理的名称，返回待处理列表

    使用 .processed.json 文件跟踪处理状态，
    同时兼容旧版通过文件名判断的方式（自动迁移）
    """
    target_path = os.path.join(output_dir, plugin_name)
    progress_file = _get_progress_file(output_dir, plugin_name)

    if not os.path.exists(target_path):
        os.makedirs(target_path, exist_ok=True)
        logger.warning(f"No existing results found. Created {target_path}")
        return input_names

    # 加载已处理名称
    processed = _load_processed_names(progress_file)

    # 兼容旧版：如果 JSON 为空但目录有文件，从现有文件名迁移
    if not processed:
        exist_files = [f for f in os.listdir(target_path) if f.endswith(".png")]
        if exist_files:
            for fname in exist_files:
                base = fname.rsplit(".", 1)[0]  # 去掉 .png
                # 提取原始名称（去掉 " - 异常" / " - 系统异常" 等后缀）
                if " - " in base:
                    name = base.split(" - ")[0].strip()
                else:
                    name = base.strip()
                processed.add(name)
            # 保存迁移结果
            with open(progress_file, "w", encoding="utf-8") as f:
                json.dump(sorted(processed), f, ensure_ascii=False, indent=2)
            logger.info(f"Migrated {len(processed)} names from existing files to progress tracker")

    logger.info(f"Found {len(processed)} already processed names")
    need_fetch_names = [n for n in input_names if n not in processed]
    return need_fetch_names


def _get_font(size: int = 33):
    """获取字体，按优先级尝试多个路径"""
    font_candidates = [
        "STHeiti Light.ttc",                          # macOS
        "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",  # Linux (Noto CJK)
        "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
        "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",           # Linux (WenQuanYi)
    ]
    for font_path in font_candidates:
        try:
            return ImageFont.truetype(font_path, size, encoding="unic")
        except (OSError, IOError):
            continue
    logger.warning("No CJK font found, falling back to default font")
    return ImageFont.load_default()


def watermark(
    image_bytes, watermark_text: str, position: Tuple, filled_color: str = "black"
) -> bytes:
    """
    给图片添加水印

    Args:
        image_bytes: 原始图片字节数据
        watermark_text: 水印文字
        position: 水印位置 (x, y)
        filled_color: 水印颜色

    Returns:
        添加水印后的 PNG 图片字节数据
    """
    font = _get_font(33)
    image = Image.open(io.BytesIO(image_bytes))
    width, height = image.size
    if width <= position[0] or height <= position[1]:
        logger.warning(
            f"Watermark position {position} exceeds image size {width}x{height}, "
            "adjusting to (10, 10)"
        )
        position = (10, 10)
    drawing = ImageDraw.Draw(image)
    drawing.text(xy=position, text=watermark_text, fill=filled_color, font=font)
    buffered = io.BytesIO()
    image.save(buffered, format="PNG")
    return buffered.getvalue()


def capture_screenshot(
    page: Page,
    plugin_name: str,
    file_name: str,
    output_dir: str,
    position: Tuple,
    filled_color: str,
):
    """使用 Playwright 截图并添加水印后保存"""
    screenshot_bytes = page.screenshot(full_page=True)

    timestamp = datetime.now().strftime("%Y-%m-%d")
    image_data = watermark(
        image_bytes=screenshot_bytes,
        watermark_text=f"{timestamp} - {file_name}",
        position=position,
        filled_color=filled_color,
    )
    with open(f"{output_dir}/{plugin_name}/{file_name}.png", "wb") as file:
        file.write(image_data)
