import time
from unittest.mock import MagicMock, patch

import pytest
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

from law_assistant.plugins.utils import (
    _get_progress_file,
    _load_processed_names,
    _save_processed_name,
    capture_screenshot,
    check_no_records_found,
    fetch_names,
    generate_names,
    retry_on_failure,
    safe_click,
    safe_fill,
    safe_get_text,
    watermark,
    _get_font,
)


# ==================== retry_on_failure ====================


class TestRetryOnFailure:
    def test_succeeds_first_attempt(self):
        call_count = 0

        @retry_on_failure(max_retries=3, delay=0)
        def succeed():
            nonlocal call_count
            call_count += 1
            return "ok"

        assert succeed() == "ok"
        assert call_count == 1

    @patch("law_assistant.plugins.utils.time.sleep")
    def test_retries_on_failure(self, mock_sleep):
        call_count = 0

        @retry_on_failure(max_retries=3, delay=0.01, backoff=1.0)
        def flaky():
            nonlocal call_count
            call_count += 1
            if call_count < 3:
                raise ValueError("fail")
            return "ok"

        assert flaky() == "ok"
        assert call_count == 3

    @patch("law_assistant.plugins.utils.time.sleep")
    def test_raises_after_max_retries(self, mock_sleep):
        @retry_on_failure(max_retries=2, delay=0.01)
        def always_fail():
            raise RuntimeError("boom")

        with pytest.raises(RuntimeError, match="boom"):
            always_fail()

    @patch("law_assistant.plugins.utils.time.sleep")
    def test_backoff_delay(self, mock_sleep):
        @retry_on_failure(max_retries=3, delay=1, backoff=2.0)
        def always_fail():
            raise RuntimeError("boom")

        with pytest.raises(RuntimeError):
            always_fail()

        # delay=1, then 1*2=2 => two sleep calls
        assert mock_sleep.call_count == 2
        mock_sleep.assert_any_call(1)
        mock_sleep.assert_any_call(2)


# ==================== safe_click ====================


class TestSafeClick:
    def test_click_success(self, mock_page):
        safe_click(mock_page, "selector", timeout=5000, retries=2)
        mock_page.click.assert_called_once_with("selector", timeout=5000)

    def test_click_retry_then_success(self, mock_page):
        mock_page.click.side_effect = [PlaywrightTimeoutError("timeout"), None]
        safe_click(mock_page, "selector", timeout=5000, retries=2)
        assert mock_page.click.call_count == 2

    def test_click_raises_after_retries(self, mock_page):
        mock_page.click.side_effect = PlaywrightTimeoutError("timeout")
        with pytest.raises(RuntimeError, match="Failed to click"):
            safe_click(mock_page, "selector", timeout=5000, retries=2)


# ==================== safe_fill ====================


class TestSafeFill:
    def test_fill_success(self, mock_page):
        safe_fill(mock_page, "selector", "text")
        mock_page.fill.assert_called_once_with("selector", "text", timeout=10000)

    def test_fill_timeout_raises(self, mock_page):
        mock_page.fill.side_effect = PlaywrightTimeoutError("timeout")
        with pytest.raises(RuntimeError, match="Timeout filling"):
            safe_fill(mock_page, "selector", "text")

    def test_fill_exception_raises(self, mock_page):
        mock_page.fill.side_effect = Exception("unexpected")
        with pytest.raises(RuntimeError, match="Error filling"):
            safe_fill(mock_page, "selector", "text")


# ==================== safe_get_text ====================


class TestSafeGetText:
    def test_get_text_success(self, mock_page):
        result = safe_get_text(mock_page, "selector")
        assert result == "some text"

    def test_get_text_timeout_returns_none(self, mock_page):
        locator = mock_page.locator.return_value
        locator.wait_for.side_effect = PlaywrightTimeoutError("timeout")
        result = safe_get_text(mock_page, "selector")
        assert result is None

    def test_get_text_exception_returns_none(self, mock_page):
        locator = mock_page.locator.return_value
        locator.wait_for.side_effect = Exception("unexpected")
        result = safe_get_text(mock_page, "selector")
        assert result is None


# ==================== check_no_records_found ====================


class TestCheckNoRecordsFound:
    def test_no_records_found(self, mock_page):
        locator = mock_page.locator.return_value
        locator.text_content.return_value = "抱歉，没找到相关结果"
        result = check_no_records_found(
            mock_page, "selector", ["没找到相关结果"]
        )
        assert result == (True, False)

    def test_records_found(self, mock_page):
        locator = mock_page.locator.return_value
        locator.text_content.return_value = "找到 3 条记录"
        result = check_no_records_found(
            mock_page, "selector", ["没找到相关结果"]
        )
        assert result == (False, False)

    def test_system_error_when_text_none(self, mock_page):
        locator = mock_page.locator.return_value
        locator.wait_for.side_effect = PlaywrightTimeoutError("timeout")
        result = check_no_records_found(
            mock_page, "selector", ["没找到"]
        )
        assert result == (False, True)


# ==================== fetch_names ====================


class TestFetchNames:
    def test_reads_and_strips_names(self, tmp_path):
        f = tmp_path / "names.txt"
        f.write_text("张三\n李四\n王五\n")
        result = fetch_names(str(f))
        assert result == ["张三", "李四", "王五"]

    def test_empty_file(self, tmp_path):
        f = tmp_path / "empty.txt"
        f.write_text("")
        result = fetch_names(str(f))
        assert result == []


# ==================== progress tracking ====================


class TestProgressTracking:
    def test_save_and_load_processed_names(self, tmp_path):
        progress_file = str(tmp_path / ".processed.json")
        _save_processed_name(progress_file, "张三")
        _save_processed_name(progress_file, "李四")
        result = _load_processed_names(progress_file)
        assert result == {"张三", "李四"}

    def test_load_nonexistent_returns_empty(self, tmp_path):
        result = _load_processed_names(str(tmp_path / "nonexistent.json"))
        assert result == set()

    def test_save_incremental(self, tmp_path):
        progress_file = str(tmp_path / ".processed.json")
        _save_processed_name(progress_file, "张三")
        _save_processed_name(progress_file, "李四")
        _save_processed_name(progress_file, "张三")  # duplicate
        result = _load_processed_names(progress_file)
        assert result == {"张三", "李四"}


# ==================== generate_names ====================


class TestGenerateNames:
    def test_new_directory_returns_all(self, tmp_path):
        output_dir = str(tmp_path)
        names = ["张三", "李四"]
        result = generate_names(names, output_dir, "test_plugin")
        assert result == names
        assert (tmp_path / "test_plugin").is_dir()

    def test_filters_processed_names(self, tmp_path):
        plugin_dir = tmp_path / "test_plugin"
        plugin_dir.mkdir()
        progress_file = str(plugin_dir / ".processed.json")
        _save_processed_name(progress_file, "张三")

        result = generate_names(["张三", "李四", "王五"], str(tmp_path), "test_plugin")
        assert result == ["李四", "王五"]

    def test_migrates_from_old_files(self, tmp_path):
        plugin_dir = tmp_path / "test_plugin"
        plugin_dir.mkdir()
        (plugin_dir / "张三.png").write_bytes(b"fake")
        (plugin_dir / "李四.png").write_bytes(b"fake")

        result = generate_names(
            ["张三", "李四", "王五"], str(tmp_path), "test_plugin"
        )
        assert result == ["王五"]
        # Check migration file was created
        assert (plugin_dir / ".processed.json").exists()

    def test_migrates_abnormal_names(self, tmp_path):
        plugin_dir = tmp_path / "test_plugin"
        plugin_dir.mkdir()
        (plugin_dir / "张三 - 异常.png").write_bytes(b"fake")

        result = generate_names(
            ["张三", "李四"], str(tmp_path), "test_plugin"
        )
        assert result == ["李四"]


# ==================== _get_font ====================


class TestGetFont:
    def test_returns_font_object(self):
        font = _get_font(size=20)
        assert font is not None


# ==================== watermark ====================


class TestWatermark:
    def test_adds_watermark_returns_bytes(self, sample_image_bytes):
        result = watermark(sample_image_bytes, "test", (10, 10))
        assert isinstance(result, bytes)
        assert len(result) > 0
        # Verify it's valid PNG
        from PIL import Image
        import io
        img = Image.open(io.BytesIO(result))
        assert img.format == "PNG"

    def test_adjusts_out_of_bounds_position(self, sample_image_bytes):
        # 50x50 image, position (100, 100) is out of bounds
        result = watermark(sample_image_bytes, "test", (100, 100))
        assert isinstance(result, bytes)
        assert len(result) > 0


# ==================== capture_screenshot ====================


class TestCaptureScreenshot:
    def test_saves_png_file(self, mock_page, tmp_output_dir):
        capture_screenshot(
            page=mock_page,
            plugin_name="csrc",
            file_name="test_capture",
            output_dir=tmp_output_dir,
            position=(10, 10),
            filled_color="black",
        )
        import os
        output_file = os.path.join(tmp_output_dir, "csrc", "test_capture.png")
        assert os.path.exists(output_file)
        assert os.path.getsize(output_file) > 0
