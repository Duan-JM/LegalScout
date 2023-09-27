import base64
from functools import partial
from multiprocessing import Pool
import time

import cv2
from loguru import logger
import numpy as np
from selenium import webdriver
from selenium.webdriver.common.action_chains import ActionChains
from selenium.webdriver.common.by import By
from tqdm import tqdm

from .utils import capture_screenshot, fetch_names, generate_names, return_opt

PLUGIN_NAME = "shixin_csrc"
MANUAL_OFFSET = 40  # fix logo width
MAX_SLIP_FAILED_CNT = 5
POSITION = (60, 120)
FILLED_COLOR = "black"


def find_slide_position(background_img):
    """use bounding box to detect target position"""
    image = cv2.cvtColor(background_img, cv2.COLOR_BGR2RGB)  # Converting BGR to RGB
    canny = cv2.Canny(image, 500, 700)

    # ==> find bounding box
    contours, _ = cv2.findContours(canny, cv2.RETR_CCOMP, cv2.CHAIN_APPROX_SIMPLE)
    dx, width = 0, 0
    for _, contour in enumerate(contours):
        x, y, w, h = cv2.boundingRect(contour)
        if (w > 30) and (h > 30):
            dx = x
            width = w
            cv2.rectangle(image, (x, y), (x + w, y + h), (0, 0, 255), 2)
    return dx, width


def verify_slip_capture(webdriver):
    time.sleep(1)
    background_img = webdriver.find_element(
        by=By.XPATH,
        value="/html/body/div/div/div/div[3]/div/div[2]/div/div[1]/div/img",
    )
    raw_image_data = background_img.get_attribute("src").split(",")[1]
    nparr = np.frombuffer(base64.b64decode(raw_image_data), np.uint8)
    img = cv2.imdecode(nparr, cv2.IMREAD_COLOR)
    move_dx, _ = find_slide_position(img)

    # ==> slide button
    btn = webdriver.find_element(
        by=By.XPATH,
        value="/html/body/div/div/div/div[3]/div/div[2]/div/div[2]/div/div",
    )
    move = ActionChains(webdriver)
    move.click_and_hold(btn)
    move.move_by_offset(move_dx + MANUAL_OFFSET, 0)
    move.release(btn)
    move.perform()
    time.sleep(3)


def find_evidence_func(name: str, output_dir: str):
    """find evidence func"""
    driver = webdriver.Chrome(options=return_opt()[0])
    driver.implicitly_wait(3)
    driver.get(f"https://neris.csrc.gov.cn/shixinchaxun/")

    # status init
    slip_capture_verified_failed = False

    # click
    time.sleep(1)
    search_inbox = driver.find_element(
        by=By.XPATH,
        value="/html/body/div/div/div/div[2]/div/div[2]/form/div[1]/div/div/input",
    )
    search_inbox.send_keys(name)

    #  TODO(Duan-JM): maybe later we need input id card
    # id_card_input = driver.find_element(
    #     by=By.XPATH, value="/html/body/div/div/div/div[2]/div/div[2]/form/div[2]/div/div/input"
    # )
    # id_card_input.send_keys(id_card)

    time.sleep(1)
    manual_bogo = driver.find_element(
        by=By.XPATH,
        value="/html/body/div/div/div/div[2]/div/div[2]/form/div[3]/div/div",
    )
    manual_bogo.click()

    failed_cnt = 0
    while len(driver.window_handles) == 1 and failed_cnt < MAX_SLIP_FAILED_CNT:
        verify_slip_capture(webdriver=driver)
        failed_cnt += 1

    if len(driver.window_handles) > 1 and failed_cnt < MAX_SLIP_FAILED_CNT:
        # switch to jump out windows
        driver.switch_to.window(driver.window_handles[1])
    else:
        slip_capture_verified_failed = True
        file_name = name + " - 验证验证失败"
        capture_screenshot(
            webdriver=driver,
            plugin_name=PLUGIN_NAME,
            file_name=file_name,
            output_dir=output_dir,
            position=POSITION,
            filled_color=FILLED_COLOR,
        )
        driver.quit()

    if not slip_capture_verified_failed:
        # check
        system_error_flag = False
        find_normal_flag = False
        try:
            time.sleep(2)
            find_text = driver.find_element(
                by=By.XPATH,
                value="/html/body/div/div/div/div[4]/div[2]/ul/li/div[2]",
            )
            if "无符合条件记录" in find_text.text:
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
