from functools import partial
from multiprocessing import Pool
from time import time
import time

from doraemon.logger.slogger import create_logger
from selenium import webdriver
from selenium.webdriver.common.by import By
from tqdm import tqdm

from law_assistant.plugins.utils import capture_screenshot, fetch_names, generate_names, return_opt

PLUGIN_NAME = "上交所信息披露"
POSITION = (40, 60)
FILLED_COLOR = "black"


logger = create_logger(__name__)

def find_evidence_func(name: str, output_dir: str):
    driver = webdriver.Chrome(options=return_opt()[0])
    driver.implicitly_wait(9)

    driver.get(f"http://www.sse.com.cn/home/search/index.shtml")

    # step 01: input name
    search_input = driver.find_element(
        by=By.XPATH, value="/html/body/div[8]/div/div[1]/div/div[1]/div/div[1]/input[12]"
    )
    search_input.send_keys(name)
    time.sleep(1)

    # step 03: click 监管
    search_button = driver.find_element(
        by=By.XPATH, value="/html/body/div[8]/div/div[1]/div/div[2]/div/div/span[6]"
    )
    search_button.click()
    time.sleep(1)

    # step 03: click 精准搜索
    search_button = driver.find_element(
        by=By.XPATH, value="/html/body/div[8]/div/div[2]/div[1]/div[6]/div[1]/div/div/div/div[1]/div/div[1]/span[1]"
    )
    search_button.click()
    time.sleep(1)

    # step 04: 搜索
    search_button = driver.find_element(
        by=By.XPATH, value="/html/body/div[8]/div/div[1]/div/div[1]/div/div[1]/input[13]"
    )
    search_button.click()
    time.sleep(3)

    # check
    system_error_flag = False
    find_normal_flag = False
    try:
        no_find_text = driver.find_element(
            by=By.XPATH, value="/html/body/div[8]/div/div[2]/div[1]/div[6]/div[2]/ul/li"
        )
        find_normal_flag = "没有找到您" in no_find_text.text
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
