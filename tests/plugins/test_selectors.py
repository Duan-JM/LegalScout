from law_assistant.plugins.selectors import (
    CSRCSelectors,
    SSESelectors,
    SZSESelectors,
    ShixinCSRCSelectors,
)


class TestCSRCSelectors:
    def test_csrc_has_required_attributes(self):
        assert hasattr(CSRCSelectors, "BASE_URL")
        assert hasattr(CSRCSelectors, "SEARCH_INPUT")
        assert hasattr(CSRCSelectors, "RESULT_TEXT")
        assert hasattr(CSRCSelectors, "SUCCESS_KEYWORDS")
        assert isinstance(CSRCSelectors.SUCCESS_KEYWORDS, list)
        assert len(CSRCSelectors.SUCCESS_KEYWORDS) > 0


class TestSSESelectors:
    def test_sse_has_required_attributes(self):
        assert hasattr(SSESelectors, "BASE_URL")
        assert hasattr(SSESelectors, "SEARCH_INPUT")
        assert hasattr(SSESelectors, "RESULT_TEXT")
        assert hasattr(SSESelectors, "SUCCESS_KEYWORDS")
        assert isinstance(SSESelectors.SUCCESS_KEYWORDS, list)


class TestSZSESelectors:
    def test_szse_has_required_attributes(self):
        assert hasattr(SZSESelectors, "BASE_URL")
        assert hasattr(SZSESelectors, "SEARCH_INPUT")
        assert hasattr(SZSESelectors, "RESULT_TEXT")
        assert hasattr(SZSESelectors, "SUCCESS_KEYWORDS")
        assert isinstance(SZSESelectors.SUCCESS_KEYWORDS, list)


class TestShixinSelectors:
    def test_shixin_has_required_attributes(self):
        assert hasattr(ShixinCSRCSelectors, "BASE_URL")
        assert hasattr(ShixinCSRCSelectors, "RESULT_TEXT")
        assert hasattr(ShixinCSRCSelectors, "SUCCESS_KEYWORDS")
        assert hasattr(ShixinCSRCSelectors, "CAPTCHA_IMAGE")
        assert hasattr(ShixinCSRCSelectors, "SLIDER_BUTTON")
        assert hasattr(ShixinCSRCSelectors, "MANUAL_OFFSET")
        assert isinstance(ShixinCSRCSelectors.MANUAL_OFFSET, int)


class TestAllBaseUrls:
    def test_all_base_urls_are_valid(self):
        for cls in (CSRCSelectors, SSESelectors, SZSESelectors, ShixinCSRCSelectors):
            assert cls.BASE_URL.startswith("http://") or cls.BASE_URL.startswith(
                "https://"
            ), f"{cls.__name__}.BASE_URL must start with http:// or https://"
