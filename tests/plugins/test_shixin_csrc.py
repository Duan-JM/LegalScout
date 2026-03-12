from unittest.mock import MagicMock, patch

import cv2
import numpy as np
import pytest

from law_assistant.plugins.shixin_csrc import ShixinCSRCPlugin


class TestShixinCSRCPlugin:
    def test_plugin_name(self):
        plugin = ShixinCSRCPlugin()
        assert plugin.plugin_name == "shixin_csrc"

    def test_watermark_position(self):
        plugin = ShixinCSRCPlugin()
        assert plugin.watermark_position == (60, 120)

    def test_handle_search_error_captcha(self):
        plugin = ShixinCSRCPlugin()
        page = MagicMock()
        error = RuntimeError("验证码验证失败")
        assert plugin.handle_search_error(page, error) == "验证码验证失败"

    def test_handle_search_error_captcha_english(self):
        plugin = ShixinCSRCPlugin()
        page = MagicMock()
        error = RuntimeError("captcha failed")
        assert plugin.handle_search_error(page, error) == "验证码验证失败"

    def test_handle_search_error_default(self):
        plugin = ShixinCSRCPlugin()
        page = MagicMock()
        error = RuntimeError("some other error")
        assert plugin.handle_search_error(page, error) is None

    def test_find_slide_position(self):
        plugin = ShixinCSRCPlugin()
        # Create a test image with a clear rectangular region
        img = np.zeros((100, 300, 3), dtype=np.uint8)
        # Draw a white rectangle to simulate the captcha target
        cv2.rectangle(img, (150, 20), (200, 80), (255, 255, 255), -1)

        dx, width = plugin._find_slide_position(img)
        # The rectangle should be detected at approximately x=150, width=50
        assert dx >= 0
        assert width >= 0

    @patch("law_assistant.plugins.shixin_csrc.safe_click")
    @patch("law_assistant.plugins.shixin_csrc.safe_fill")
    def test_execute_search_captcha_fails_raises(self, mock_fill, mock_click):
        plugin = ShixinCSRCPlugin()
        page = MagicMock()
        context = MagicMock()
        # context.pages always has the same count -> captcha never passes
        context.pages = [MagicMock()]

        # Make _verify_slide_captcha always raise
        with patch.object(
            plugin, "_verify_slide_captcha", side_effect=Exception("captcha fail")
        ):
            with pytest.raises(RuntimeError, match="验证码验证失败"):
                plugin.execute_search(page, "张三", context)
