package browser

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/sources"
)

func TestNormalizeSelectorSupportsXPathAndCSS(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		kind selectorKind
	}{
		{"xpath=//main//li[1]", "//main//li[1]", selectorXPath},
		{" #result .entry ", "#result .entry", selectorCSS},
	}
	for _, test := range cases {
		got, kind, err := normalizeSelector(test.raw)
		if err != nil || got != test.want || kind != test.kind {
			t.Fatalf("normalizeSelector(%q) = %q, %q, %v", test.raw, got, kind, err)
		}
	}
	if _, _, err := normalizeSelector("xpath= "); err == nil {
		t.Fatal("empty XPath must be rejected")
	}
}

func TestProviderActionPlanUsesBothSelectorReadPaths(t *testing.T) {
	source, ok := sources.ByID("szse_disclosure")
	if !ok {
		t.Fatal("missing szse source")
	}
	plan, err := providerActionPlan(source)
	if err != nil {
		t.Fatal(err)
	}
	var baseline string
	if actions, err := providerActions(source, "张三", &baseline); err != nil || len(actions) == 0 {
		t.Fatalf("provider actions = %d, %v", len(actions), err)
	}
	var sawCSSInput, sawCSSResult bool
	for _, step := range plan {
		if strings.HasPrefix(step.Selector, "xpath=") {
			t.Fatalf("raw Playwright prefix leaked into action plan: %#v", step)
		}
		if step.Operation == "wait" && step.Kind == selectorCSS {
			sawCSSInput = true
		}
		if step.Operation == "text" && step.Kind == selectorCSS {
			sawCSSResult = true
		}
	}
	if !sawCSSInput || !sawCSSResult {
		t.Fatalf("action plan did not preserve CSS paths: %#v", plan)
	}
	xpathResult := source
	xpathResult.Selectors.ResultText = "xpath=//main//li[1]"
	plan, err = providerActionPlan(xpathResult)
	if err != nil {
		t.Fatal(err)
	}
	if final := plan[len(plan)-1]; final.Operation != "text" || final.Kind != selectorXPath || final.Selector != "//main//li[1]" {
		t.Fatalf("XPath result read path = %#v", final)
	}
}

func TestCSRCActionPlanScopesSearchToPenaltyChannel(t *testing.T) {
	source, ok := sources.ByID("csrc")
	if !ok {
		t.Fatal("missing csrc source")
	}
	plan, err := providerActionPlan(source)
	if err != nil {
		t.Fatal(err)
	}
	var scoped, input, result bool
	for _, step := range plan {
		switch {
		case step.Operation == "set_value" && step.Selector == "#channelid":
			scoped = true
		case step.Operation == "set_value" && step.Selector == "#content":
			input = true
		case step.Operation == "text" && step.Selector == "#codeId_list":
			result = true
		}
	}
	if !scoped || !input || !result {
		t.Fatalf("CSRC query plan is incomplete: %#v", plan)
	}
	if !source.Selectors.DirectQuery || source.Selectors.ReadyExpression != "" {
		t.Fatalf("CSRC query synchronization = ready %q direct=%v", source.Selectors.ReadyExpression, source.Selectors.DirectQuery)
	}
}

func TestRemoteEndpointsAndManualReviewAreSafe(t *testing.T) {
	ws := "wss://chrome.browserless.io?token=super-secret"
	if got, err := remoteEndpoint(ws); err != nil || got != ws {
		t.Fatalf("websocket endpoint = %q, %v", got, err)
	}

	http := "https://cdp.example.test:9222"
	if got, err := remoteEndpoint(http); err != nil || got != http {
		t.Fatalf("HTTP discovery endpoint = %q, %v", got, err)
	}
	redacted := redactRemoteURL("wss://alice:password@host/?token=super-secret&apikey=also-secret&safe=yes")
	if strings.Contains(redacted, "alice") || strings.Contains(redacted, "password") ||
		strings.Contains(redacted, "super-secret") || strings.Contains(redacted, "also-secret") {
		t.Fatalf("remote description leaked secret: %q", redacted)
	}
	if description := (&Provider{RemoteURL: "wss://alice:password@host/?token=super-secret"}).Description(); strings.Contains(description, "super-secret") || strings.Contains(description, "alice") {
		t.Fatalf("provider description leaked secret: %q", description)
	}
	source, _ := sources.ByID("shixin_csrc")
	result, err := (&Provider{RemoteURL: ws}).Run(context.Background(), source, "张三", true)
	if err == nil || result.Status != domain.NeedsReview {
		t.Fatalf("remote manual review = %#v, %v", result, err)
	}
}

func TestRemoteURLRedactsEveryQueryValue(t *testing.T) {
	redacted := redactRemoteURL("https://host/?x-api-key=third-secret&safe=yes")
	if strings.Contains(redacted, "third-secret") || strings.Contains(redacted, "yes") {
		t.Fatalf("remote description leaked query value: %q", redacted)
	}
}

func TestHTTPSRemoteDiscoveryPreservesEncryptionAndCredentials(t *testing.T) {
	server := httptest.NewTLSServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		if request.URL.Path != "/json/version" || request.URL.Query().Get("token") != "super-secret" {
			t.Fatalf("discovery request = %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/test"}`))
	}))
	defer server.Close()
	originalClient := stdhttp.DefaultClient
	stdhttp.DefaultClient = server.Client()
	defer func() { stdhttp.DefaultClient = originalClient }()

	endpoint, err := discoverRemoteEndpoint(context.Background(), server.URL+"?token=super-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(endpoint, "wss://") || !strings.Contains(endpoint, "token=super-secret") ||
		!strings.Contains(endpoint, "/devtools/browser/test") {
		t.Fatalf("secure discovery endpoint = %q", endpoint)
	}
}

func TestRemoteErrorsDoNotLeakCredentials(t *testing.T) {
	provider := &Provider{RemoteURL: "wss://user:password@host/?x-api-key=third-secret"}
	sanitized := provider.sanitizeRemoteError(
		errors.New("dial wss://user:password@host/?x-api-key=third-secret failed for third-secret"),
	)
	for _, secret := range []string{"user", "password", "third-secret"} {
		if strings.Contains(sanitized.Error(), secret) {
			t.Fatalf("remote error leaked %q: %v", secret, sanitized)
		}
	}
}

func TestDecodeCSRCSearchRejectsIncompleteSuccessPayloads(t *testing.T) {
	if _, err := decodeCSRCSearch([]byte(`{"code":200,"data":{"total":0,"results":[]}}`)); err != nil {
		t.Fatalf("valid empty result rejected: %v", err)
	}

	for _, raw := range []string{
		`{"code":200}`,
		`{"code":200,"data":null}`,
		`{"code":200,"data":{"results":[]}}`,
		`{"code":200,"data":{"total":null,"results":[]}}`,
		`{"code":200,"data":{"total":0}}`,
		`{"code":200,"data":{"total":1,"results":[]}}`,
	} {
		if _, err := decodeCSRCSearch([]byte(raw)); err == nil {
			t.Fatalf("incomplete CSRC response accepted: %s", raw)
		}
	}
}

func TestTemporaryPageClassificationRemainsRetryable(t *testing.T) {
	source, _ := sources.ByID("sse_disclosure")
	status, err := source.Classify("服务繁忙，请稍后重试")
	if err == nil || status != domain.RetryableError {
		t.Fatalf("temporary page = %s, %v", status, err)
	}
	result := Result{Status: status}
	if result.Status != domain.RetryableError {
		t.Fatalf("browser result lost retryable status: %s", result.Status)
	}
}
