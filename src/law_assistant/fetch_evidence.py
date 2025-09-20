import argparse
import os
from typing import List

import structlog
from doraemon.logger.slogger import configure_structlog

from law_assistant.constants import AVALIABLE_SOURCES_FUNCS

configure_structlog()
logger = structlog.getLogger(__name__)


def main(input_file: str, source_list: List[str], output_dir: str, process_num: int):
    # Step 01: Check Avaliable Inputs
    assert os.path.exists(input_file), f"{input_file} not exist"
    assert os.path.exists(output_dir), f"{output_dir} not exist"
    assert all(s in AVALIABLE_SOURCES_FUNCS.keys() for s in source_list)

    # Step 02: Generate Results
    for idx, source in enumerate(source_list):
        logger.info(f"Starting fetch from {source} with input_file {input_file};")
        AVALIABLE_SOURCES_FUNCS[source](
            input_file=input_file, output_dir=output_dir, process_num=process_num
        )
        logger.info(
            f"Finished fetch from {source}, {len(source_list) - idx - 1} remained"
        )
    logger.info(f"Finished, please check your result in {output_dir}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--input_file", help="Name file")
    parser.add_argument("--sources", help="Sources")
    parser.add_argument("--output_dir", help="Output")
    parser.add_argument("--process_num", type=int, help="process_num")
    args = parser.parse_args()

    main(
        input_file=args.input_file,
        source_list=[x.strip() for x in args.sources.split(",")],
        output_dir=args.output_dir,
        process_num=args.process_num,
    )
