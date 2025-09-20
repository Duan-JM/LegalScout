from functools import partial
from multiprocessing import Pool
from time import time
import time

import structlog
from selenium import webdriver
from selenium.webdriver.common.by import By
from tqdm import tqdm

from law_assistant.plugins.utils import capture_screenshot, fetch_names, generate_names, return_opt

logger = structlog.getLogger(__name__)
PLUGIN_NAME = "深交所信息披露"
POSITION = (40, 60)
FILLED_COLOR = "black"



def find_evidence_func(name: str, output_dir: str):
    driver = webdriver.Chrome(options=return_opt()[0])

    driver.get("http://www.szse.cn/disclosure/supervision/measure/pushish/index.html")
    driver.implicitly_wait(3)
    time.sleep(6)
    input_box = driver.find_element(by=By.ID, value="1800_jgxxgk_cf_tab2_txtBj")
    input_box.send_keys(name)
    button = driver.find_element(
        by=By.XPATH,
        value="/html/body/div[5]/div/div[2]/div/div/div[2]/div/div[7]/button",
    )
    button.click()
    time.sleep(3)

    # check
    system_error_flag = False
    find_normal_flag = False
    try:
        find_flag = driver.find_element(
            by=By.XPATH,
            value="/html/body/div[5]/div/div[2]/div/div/div[4]/div/div[2]/div/div/div[3]",
        )
        find_normal_flag = "没有找到" in find_flag.text
    except:
        system_error_flag = False

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
