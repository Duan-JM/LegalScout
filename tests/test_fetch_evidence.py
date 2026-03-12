import sys
from unittest.mock import MagicMock, patch

import pytest

# Mock the doraemon module which may not be installed in test environments
sys.modules.setdefault("doraemon", MagicMock())
sys.modules.setdefault("doraemon.logger", MagicMock())
sys.modules.setdefault("doraemon.logger.slogger", MagicMock())

from law_assistant.fetch_evidence import main


class TestFetchEvidence:
    def test_input_file_not_found(self, tmp_output_dir):
        with pytest.raises(FileNotFoundError, match="Input file not found"):
            main(
                input_file="/nonexistent/file.txt",
                source_list=["csrc"],
                output_dir=tmp_output_dir,
                process_num=1,
            )

    def test_output_dir_not_found(self, tmp_input_file):
        with pytest.raises(FileNotFoundError, match="Output directory not found"):
            main(
                input_file=tmp_input_file,
                source_list=["csrc"],
                output_dir="/nonexistent/output",
                process_num=1,
            )

    def test_invalid_source(self, tmp_input_file, tmp_output_dir):
        with pytest.raises(ValueError, match="Unknown sources"):
            main(
                input_file=tmp_input_file,
                source_list=["invalid_source"],
                output_dir=tmp_output_dir,
                process_num=1,
            )

    @patch("law_assistant.fetch_evidence.AVAILABLE_SOURCES_FUNCS")
    def test_main_calls_sources_in_order(
        self, mock_sources, tmp_input_file, tmp_output_dir
    ):
        mock_func1 = MagicMock()
        mock_func2 = MagicMock()
        mock_sources.__contains__ = lambda self, x: x in ("src1", "src2")
        mock_sources.__getitem__ = lambda self, x: {"src1": mock_func1, "src2": mock_func2}[x]
        mock_sources.keys = MagicMock(return_value=["src1", "src2"])

        main(
            input_file=tmp_input_file,
            source_list=["src1", "src2"],
            output_dir=tmp_output_dir,
            process_num=1,
        )

        mock_func1.assert_called_once_with(
            input_file=tmp_input_file,
            output_dir=tmp_output_dir,
            process_num=1,
            dev=False,
        )
        mock_func2.assert_called_once_with(
            input_file=tmp_input_file,
            output_dir=tmp_output_dir,
            process_num=1,
            dev=False,
        )
