package sources

import (
	"strings"
	"testing"

	"github.com/Duan-JM/LegalScout/internal/domain"
)

func TestAllSourceContractsAreComplete(t *testing.T) {
	all := Registry()
	if len(all) != 4 {
		t.Fatalf("sources = %d, want 4", len(all))
	}
	for _, source := range all {
		if err := source.PreflightContract(); err != nil {
			t.Fatalf("%s: %v", source.ID, err)
		}
	}
}

func TestOnlyExplicitNoRecordPhraseYieldsNotFound(t *testing.T) {
	source, _ := ByID("csrc")
	status, err := source.Classify("抱歉，没找到相关结果")
	if err != nil || status != domain.NotFound {
		t.Fatalf("status=%s err=%v", status, err)
	}

	status, err = source.Classify("页面加载失败")
	if err == nil || status == domain.NotFound {
		t.Fatalf("error page must not become not_found: status=%s err=%v", status, err)
	}
	status, err = source.Classify("")
	if err == nil || status == domain.NotFound {
		t.Fatalf("empty result must not be not_found: %s %v", status, err)
	}
	status, err = source.Classify("查询结果：张三\n系统维护中")
	if err == nil || status != domain.RetryableError {
		t.Fatalf("temporary error text must win over arbitrary record content: %s %v", status, err)
	}
	status, err = source.Classify("安全验证，请完成验证码")
	if err != nil || status != domain.NeedsReview {
		t.Fatalf("captcha result = %s %v", status, err)
	}
	status, err = source.Classify("张三 监管措施记录")
	if err != nil || status != domain.Found {
		t.Fatalf("readable non-error result must be found: %s %v", status, err)
	}
}

func TestLiveSelectorContractsAvoidInvalidNumericCSSAndBrittleCSRCXPath(t *testing.T) {
	csrc, _ := ByID("csrc")
	if csrc.Selectors.Input != "#content" || csrc.Selectors.ResultText != "#codeId_list" ||
		csrc.Selectors.SetValues["#channelid"] == "" {
		t.Fatalf("CSRC direct search contract = %#v", csrc.Selectors)
	}
	szse, _ := ByID("szse_disclosure")
	if strings.HasPrefix(szse.Selectors.Input, "#1800") ||
		szse.Selectors.ResultText != ".reporttboverfow-in" {
		t.Fatalf("SZSE selector contract = %#v", szse.Selectors)
	}
}

func TestShixinAcceptsNaturalPersonsAndOrganisations(t *testing.T) {
	source, ok := ByID("shixin_csrc")
	if !ok {
		t.Fatal("shixin_csrc missing")
	}
	if len(source.EntityTypes) != 2 || source.EntityTypes[0] != NaturalPerson || source.EntityTypes[1] != Organisation {
		t.Fatalf("entity types = %#v, want natural person and organisation", source.EntityTypes)
	}
}
