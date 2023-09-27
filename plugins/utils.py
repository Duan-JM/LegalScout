import base64
from datetime import datetime
import io
import os
from typing import Tuple

from PIL import Image, ImageDraw, ImageFont
from loguru import logger
from selenium import webdriver


def return_opt():
    chrome_options = webdriver.ChromeOptions()
    print_settings = {
        "recentDestinations": [
            {
                "id": "Save as PDF",
                "origin": "local",
                "account": "",
            }
        ],
        "selectedDestinationId": "Save as PDF",
        "version": 2,
        "isHeaderFooterEnabled": False,
        "isLandscapeEnabled": True,
    }

    chrome_options.add_argument("headless")  # hide browser
    return chrome_options, print_settings


def fetch_names(input_file):
    with open(input_file, "r") as f:
        names = f.readlines()
    return [x.strip() for x in names]


def generate_names(input_names, output_dir, plugin_name):
    target_path = f"{output_dir}/{plugin_name}"
    if os.path.exists(target_path):
        exist_names = os.listdir(target_path)
        logger.info(f"Found exist {len(exist_names)} pdf")
        exist_names = [name.split(".")[0] for name in exist_names]
        need_fetch_names = list(set(input_names) - set(exist_names))
    else:
        os.mkdir(target_path)
        logger.warning(f"No exist result found. Makedir {target_path}!")
        need_fetch_names = input_names
    return need_fetch_names


def watermark(
    image_bytes, watermark_text: str, position: Tuple, filled_color: str = "black"
):
    font = ImageFont.truetype("Adobe 楷体 Std R.otf", 33, encoding="unic")
    image = Image.open(io.BytesIO(image_bytes))
    width, height = image.size
    assert width > position[0] and height > position[1]
    drawing = ImageDraw.Draw(image)
    drawing.text(xy=position, text=watermark_text, fill=filled_color, font=font)
    buffered = io.BytesIO()
    image.save(buffered, format="PNG")
    return base64.b64encode(buffered.getvalue())


def watermark_test():
    image = Image.new("RGB", (559, 320), (255, 255, 255))
    buffered = io.BytesIO()
    image.save(buffered, format="PNG")
    return_bytes = watermark(
        buffered.getvalue(), "哈哈哈哈", position=(20, 40), filled_color="red"
    )
    image = Image.open(io.BytesIO(base64.b64decode(return_bytes)))
    image.show()


def capture_screenshot(
    webdriver,
    plugin_name: str,
    file_name: str,
    output_dir: str,
    position: Tuple,
    filled_color: str,
):
    pdf_data = webdriver.execute_cdp_cmd(
        "Page.captureScreenshot",
        cmd_args={"format": "png", "captureBeyondViewport": True},
    )
    timestamp = datetime.now().strftime("%Y-%m-%d")
    pdf_data = watermark(
        image_bytes=base64.b64decode(pdf_data["data"]),
        watermark_text=f"{timestamp} - {file_name}",
        position=position,
        filled_color=filled_color,
    )
    with open(f"{output_dir}/{plugin_name}/{file_name}.png", "wb") as file:
        file.write(base64.b64decode(pdf_data))


if __name__ == "__main__":
    watermark_test()
