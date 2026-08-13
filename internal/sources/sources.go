// Package sources declares the public contract of the four supported sites.
// Adapters contain no browser implementation so their result rules can be
// tested without visiting a live site.
package sources

import (
	"fmt"
	"strings"
	"time"

	"github.com/Duan-JM/LegalScout/internal/domain"
)

type EntityType string

const (
	NaturalPerson EntityType = "natural_person"
	Organisation  EntityType = "organisation"
)

type Selector struct {
	Input              string
	Button             string
	Before             []string
	SetValues          map[string]string
	ReadyExpression    string
	SubmitExpression   string
	StartedExpression  string
	CompleteExpression string
	DirectQuery        bool
	ResultText         string
}

type Adapter struct {
	ID                 string
	Name               string
	EntityTypes        []EntityType
	URL                string
	RequiresHuman      bool
	PreflightSelector  string
	Selectors          Selector
	NoRecordPhrases    []string
	ResultPhrases      []string
	Timeout            time.Duration
	Retries            int
	ScreenshotStrategy string
}

func (a Adapter) Classify(text string) (domain.CheckStatus, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.FatalError, fmt.Errorf("%s 页面没有返回可识别结果", a.Name)
	}
	// This method is called only after ResultText was successfully read. Its
	// contract is deliberately conservative: exact no-record text is the only
	// route to NotFound; otherwise a readable, non-error result is Found.
	for _, phrase := range []string{
		"服务繁忙", "稍后重试", "访问过于频繁", "操作过于频繁", "系统维护",
	} {
		if strings.Contains(text, phrase) {
			return domain.RetryableError, fmt.Errorf("%s 返回临时异常页面：%s", a.Name, phrase)
		}
	}
	for _, phrase := range []string{
		"系统异常", "服务异常", "加载失败", "加载出错", "请求失败", "查询失败",
		"网络异常", "网络错误", "访问限制", "无权访问", "页面不存在", "页面错误", "安全拦截",
	} {
		if strings.Contains(text, phrase) {
			return domain.FatalError, fmt.Errorf("%s 返回异常页面：%s", a.Name, phrase)
		}
	}
	for _, phrase := range []string{"验证码", "人机验证", "安全验证", "滑动验证"} {
		if strings.Contains(text, phrase) {
			return domain.NeedsReview, nil
		}
	}
	for _, phrase := range a.NoRecordPhrases {
		if strings.Contains(text, phrase) {
			return domain.NotFound, nil
		}
	}
	// Result text present but it did not state "no records"; this is a
	// positive result. A selector/load error never reaches this branch.
	return domain.Found, nil
}

func (a Adapter) PreflightContract() error {
	if a.ID == "" || a.Name == "" || !strings.HasPrefix(a.URL, "http") ||
		a.PreflightSelector == "" || a.Selectors.ResultText == "" || a.Timeout <= 0 || a.Retries < 0 {
		return fmt.Errorf("source contract is incomplete for %q", a.ID)
	}
	if !a.RequiresHuman && (a.Selectors.Input == "" ||
		(a.Selectors.Button == "" && a.Selectors.SubmitExpression == "" && !a.Selectors.DirectQuery)) {
		return fmt.Errorf("source contract is missing query controls for %q", a.ID)
	}
	return nil
}

func Registry() []Adapter {
	return []Adapter{
		{
			ID: "csrc", Name: "证监会政府信息公开", EntityTypes: []EntityType{NaturalPerson, Organisation},
			URL: "https://www.csrc.gov.cn/csrc/c101971/zfxxgk_zdgk.shtml",
			Selectors: Selector{
				SetValues: map[string]string{
					"#channelid": "17d5ff2fe43e488dba825807ae40d63f",
				},
				Input:       "#content",
				DirectQuery: true,
				ResultText:  "#codeId_list",
			},
			PreflightSelector: "#content",
			NoRecordPhrases:   []string{"抱歉，没找到相关结果"}, Timeout: 45 * time.Second, Retries: 2, ScreenshotStrategy: "full_page",
		},
		{
			ID: "sse_disclosure", Name: "上交所监管信息", EntityTypes: []EntityType{NaturalPerson, Organisation},
			URL: "https://www.sse.com.cn/home/search/index.shtml",
			Selectors: Selector{
				Before: []string{
					"xpath=/html/body/div[9]/div/div[1]/div/div[2]/div/div/span[5]",
					"xpath=/html/body/div[9]/div/div[2]/div[1]/div[6]/div[1]/div/div/div/div[1]/div/div[1]",
				},
				Input:      "xpath=/html/body/div[9]/div/div[1]/div/div[1]/div/div[1]/input[12]",
				Button:     "xpath=/html/body/div[9]/div/div[1]/div/div[1]/div/div[1]/input[13]",
				ResultText: "xpath=/html/body/div[9]/div/div[2]/div[1]/div[6]/div[2]/ul/li",
			},
			PreflightSelector: "xpath=/html/body/div[9]/div/div[1]/div/div[1]/div/div[1]/input[12]",
			NoRecordPhrases:   []string{"没有找到您"}, Timeout: 40 * time.Second, Retries: 2, ScreenshotStrategy: "full_page",
		},
		{
			ID: "szse_disclosure", Name: "深交所监管信息", EntityTypes: []EntityType{NaturalPerson, Organisation},
			URL: "https://www.szse.cn/disclosure/supervision/measure/pushish/index.html",
			Selectors: Selector{
				Input:      `[id="1800_jgxxgk_cf_tab2_txtBj"]`,
				Button:     "button.confirm-query",
				ResultText: ".reporttboverfow-in",
			},
			PreflightSelector: `[id="1800_jgxxgk_cf_tab2_txtBj"]`,
			NoRecordPhrases:   []string{"没有找到符合条件的数据"}, Timeout: 40 * time.Second, Retries: 2, ScreenshotStrategy: "full_page",
		},
		{
			ID: "shixin_csrc", Name: "证券期货市场失信记录", EntityTypes: []EntityType{NaturalPerson, Organisation},
			URL: "https://neris.csrc.gov.cn/shixinchaxun/", RequiresHuman: true,
			Selectors: Selector{
				Input:      "xpath=/html/body/div/div/div/div[2]/div/div[2]/form/div[1]/div/div/input",
				Button:     "xpath=/html/body/div/div/div/div[2]/div/div[2]/form/div[3]/div/div",
				ResultText: "xpath=/html/body/div/div/div/div[4]/div[2]/ul/li/div[2]",
			},
			PreflightSelector: "xpath=/html/body/div/div/div/div[2]/div/div[2]/form/div[1]/div/div/input",
			NoRecordPhrases:   []string{"无符合条件记录"}, Timeout: 5 * time.Minute, Retries: 0, ScreenshotStrategy: "full_page",
		},
	}
}

func ByID(id string) (Adapter, bool) {
	for _, source := range Registry() {
		if source.ID == id {
			return source, true
		}
	}
	return Adapter{}, false
}

func Securities() []string {
	sources := Registry()
	result := make([]string, 0, len(sources))
	for _, source := range sources {
		result = append(result, source.ID)
	}
	return result
}
