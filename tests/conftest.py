import io
import os
from unittest.mock import MagicMock

import pytest
from PIL import Image


@pytest.fixture
def mock_page():
    """Mock Playwright Page object with common methods."""
    page = MagicMock()
    page.click = MagicMock()
    page.fill = MagicMock()
    page.goto = MagicMock()
    page.screenshot = MagicMock(return_value=_make_png_bytes(100, 100))
    page.wait_for_selector = MagicMock()
    page.wait_for_timeout = MagicMock()

    locator = MagicMock()
    locator.wait_for = MagicMock()
    locator.text_content = MagicMock(return_value="some text")
    page.locator = MagicMock(return_value=locator)

    mouse = MagicMock()
    mouse.move = MagicMock()
    mouse.down = MagicMock()
    mouse.up = MagicMock()
    page.mouse = mouse

    return page


@pytest.fixture
def mock_browser():
    """Mock Browser object."""
    browser = MagicMock()
    browser.close = MagicMock()
    browser.contexts = []
    return browser


@pytest.fixture
def mock_context():
    """Mock BrowserContext."""
    context = MagicMock()
    context.pages = [MagicMock()]
    context.new_page = MagicMock(return_value=MagicMock())
    return context


@pytest.fixture
def tmp_output_dir(tmp_path):
    """Temporary output directory with plugin subdirectories."""
    for plugin in ("csrc", "shixin_csrc", "sse_disclosure", "szse_disclosure"):
        (tmp_path / plugin).mkdir()
    return str(tmp_path)


@pytest.fixture
def tmp_input_file(tmp_path):
    """Temporary input file with sample names."""
    f = tmp_path / "names.txt"
    f.write_text("张三\n李四\n王五\n")
    return str(f)


@pytest.fixture
def sample_image_bytes():
    """Minimal 50x50 PNG image bytes for watermark tests."""
    return _make_png_bytes(50, 50)


def _make_png_bytes(width=50, height=50):
    img = Image.new("RGB", (width, height), color=(255, 255, 255))
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()
