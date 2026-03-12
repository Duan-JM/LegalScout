import argparse
import os
from typing import List

import structlog
from doraemon.logger.slogger import configure_structlog

from law_assistant.constants import AVAILABLE_SOURCES_FUNCS

configure_structlog()
logger = structlog.getLogger(__name__)


def main(
    input_file: str,
    source_list: List[str],
    output_dir: str,
    process_num: int,
    dev: bool = False,
):
    # Step 01: Check Available Inputs
    if not os.path.exists(input_file):
        raise FileNotFoundError(f"Input file not found: {input_file}")
    if not os.path.exists(output_dir):
        raise FileNotFoundError(f"Output directory not found: {output_dir}")
    invalid_sources = [s for s in source_list if s not in AVAILABLE_SOURCES_FUNCS]
    if invalid_sources:
        raise ValueError(
            f"Unknown sources: {invalid_sources}. "
            f"Available: {list(AVAILABLE_SOURCES_FUNCS.keys())}"
        )

    # Step 02: Generate Results
    for idx, source in enumerate(source_list):
        logger.info("Starting fetch", source=source, input_file=input_file)
        AVAILABLE_SOURCES_FUNCS[source](
            input_file=input_file,
            output_dir=output_dir,
            process_num=process_num,
            dev=dev,
        )
        remaining = len(source_list) - idx - 1
        logger.info("Finished fetch", source=source, remaining=remaining)
    logger.info("All done", output_dir=output_dir)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--input_file", help="Name file")
    parser.add_argument("--sources", help="Sources")
    parser.add_argument("--output_dir", help="Output")
    parser.add_argument("--debug", action="store_true", help="Debug mode")
    parser.add_argument("--process_num", type=int, help="process_num")
    args = parser.parse_args()

    main(
        input_file=args.input_file,
        source_list=[x.strip() for x in args.sources.split(",")],
        output_dir=args.output_dir,
        process_num=args.process_num,
        dev=args.debug,
    )
