from functools import partial
from multiprocessing import Pool
from time import time
import time

from loguru import logger
from selenium import webdriver
from selenium.webdriver.common.by import By
from tqdm import tqdm

from .utils import (
    capture_screenshot,
    fetch_names,
    generate_names,
    return_opt,
)

PLUGIN_NAME = "上交所信息披露"
POSITION = (40, 60)
FILLED_COLOR = "black"


def find_evidence_func(name: str, output_dir: str):
    driver = webdriver.Chrome(options=return_opt()[0])
    driver.implicitly_wait(9)

    driver.get(f"http://www.sse.com.cn/home/search/index_old.shtml")

    # step 01: click jianguan
    jg_button = driver.find_element(
        by=By.XPATH, value="/html/body/div[7]/div/div[2]/div[1]/div[1]/span[6]"
    )
    time.sleep(2)
    jg_button.click()

    # step 02: click
    search_input = driver.find_element(
        by=By.XPATH, value="/html/body/div[7]/div/div[1]/div/div/div/div[1]/input[12]"
    )
    search_input.send_keys(name)
    time.sleep(2)

    # step 03: click button
    search_button = driver.find_element(
        by=By.XPATH, value="/html/body/div[7]/div/div[1]/div/div/div/div[1]/input[13]"
    )
    search_button.click()
    time.sleep(5)

    # check
    system_error_flag = False
    find_normal_flag = False
    try:
        no_find_text = driver.find_element(
            by=By.XPATH, value="/html/body/div[7]/div/div[2]/div[2]/div[6]/div[3]/ul/li"
        )
        find_normal_flag = "没有找到您要找的内容" in no_find_text.text
    except:
        system_error_flag = True

    if not system_error_flag:
        if find_normal_flag:
            file_name = name
        else:
            file_name = name + " - 异常"
            logger.warning(f"Abnoraml Found - {file_name}")
    else:
        file_name = name + " - 系统异常"
        logger.error(f"Abnoraml Found - {file_name}")

    # save screeshot
    capture_screenshot(
        webdriver=driver,
        plugin_name=PLUGIN_NAME,
        file_name=file_name,
        output_dir=output_dir,
        position=POSITION,
        filled_color=FILLED_COLOR,
    )
    driver.quit()


def api_v1(input_file: str, output_dir: str, process_num: int = 10):
    require_names = fetch_names(input_file)
    names = generate_names(
        input_names=require_names, output_dir=output_dir, plugin_name=PLUGIN_NAME
    )
    pbar = tqdm(total=len(names))
    with Pool(processes=process_num) as pool:
        for _ in pool.imap_unordered(
            partial(find_evidence_func, output_dir=output_dir), names
        ):
            pbar.update(1)
