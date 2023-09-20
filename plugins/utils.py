import os

from loguru import logger
from selenium import webdriver


def return_opt():
    chrome_options = webdriver.chrome.options.Options()
    print_settings = {
        "recentDestinations": [{
            "id": "Save as PDF",
            "origin": "local",
            "account": "",
        }],
        "selectedDestinationId": "Save as PDF",
        "version": 2,
        "isHeaderFooterEnabled": False,
        "isLandscapeEnabled": True
    }

    chrome_options.add_argument("headless")  # hide browser
    return chrome_options, print_settings


def fetch_names(input_file):
    with open(input_file, 'r') as f:
        names = f.readlines()
    return [x.strip() for x in names]


def generate_names(input_names, output_dir, plugin_name):
    target_path = f'{output_dir}/{plugin_name}'
    if os.path.exists(target_path):
        exist_names = os.listdir(target_path)
        logger.info(f"Found exist {len(exist_names)} pdf")
        exist_names = [name.split('.')[0] for name in exist_names]
        need_fetch_names = list(set(input_names) - set(exist_names))
    else:
        os.mkdir(target_path)
        logger.warning(f"No exist result found. Makedir {target_path}!")
        need_fetch_names = input_names
    return need_fetch_names
