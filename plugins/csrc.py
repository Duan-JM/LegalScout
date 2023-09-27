from functools import partial
from multiprocessing import Pool
import time

from loguru import logger
from selenium import webdriver
from selenium.webdriver.common.by import By
from tqdm import tqdm

from .utils import capture_screenshot, fetch_names, generate_names, return_opt

PLUGIN_NAME = "csrc"


def find_evidence_func(name: str, output_dir: str):
    driver = webdriver.Chrome(options=return_opt()[0])
    driver.implicitly_wait(3)

    driver.get(f"http://www.csrc.gov.cn/csrc/c100033/zfxxgk_zdgk.shtml#tab=gkzn")

    # click
    time.sleep(1)
    manual_bogo = driver.find_element(
        by=By.XPATH, value="/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[1]"
    )
    manual_bogo.click()

    time.sleep(1)
    manual_bogo = driver.find_element(
        by=By.XPATH,
        value="/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[2]/div[1]/div[2]",
    )
    manual_bogo.click()

    time.sleep(1)
    manual_bogo = driver.find_element(
        by=By.XPATH,
        value="/html/body/div[1]/div[3]/div[2]/ul/li[5]/div[2]/div[1]/div[2]/div/ul/li[9]/a",
    )
    manual_bogo.click()

    time.sleep(1)
    search_inbox = driver.find_element(
        by=By.XPATH, value="/html/body/div[1]/div[3]/div[1]/div[2]/div/input[3]"
    )
    search_inbox.send_keys(name)

    time.sleep(1)
    search_btn = driver.find_element(
        by=By.XPATH, value="/html/body/div[1]/div[3]/div[1]/div[2]/div/a"
    )
    search_btn.click()

    # check
    system_error_flag = False
    find_normal_flag = False
    try:
        time.sleep(2)
        find_text = driver.find_element(
            by=By.XPATH,
            value="/html/body/div[1]/div[3]/div[3]/div[5]/div[1]/div/div/div[2]/div/div[1]/ul/table/tbody/tr[2]/td",
        )
        if "抱歉，没找到相关结果" in find_text.text:
            find_normal_flag = True
    except:
        system_error_flag = False

    if not system_error_flag:
        if find_normal_flag:
            file_name = name
        else:
            file_name = name + " - 异常"
            logger.warning(f"Found abnormal {file_name}")
    else:
        file_name = name + " - 系统异常"
        logger.error(f"Abnoraml Found - {file_name}")

    # save screeshot
    capture_screenshot(
        webdriver=driver,
        plugin_name=PLUGIN_NAME,
        file_name=file_name,
        output_dir=output_dir,
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
