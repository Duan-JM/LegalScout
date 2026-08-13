// Package browser provides the single chromedp implementation shared by all
// sources. Adapters never open browsers or duplicate wait/click mechanics.
package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/Duan-JM/LegalScout/internal/domain"
	"github.com/Duan-JM/LegalScout/internal/sources"
)

type Result struct {
	Status     domain.CheckStatus
	Screenshot []byte
}

// PreflightError lets a batch scheduler distinguish a broken source contract
// from a temporary browser or network failure without parsing localized text.
type PreflightError struct {
	Status domain.CheckStatus
	Err    error
}

func (e *PreflightError) Error() string { return e.Err.Error() }
func (e *PreflightError) Unwrap() error { return e.Err }

func PreflightStatus(err error) domain.CheckStatus {
	var preflight *PreflightError
	if errors.As(err, &preflight) && preflight.Status.Validate() == nil {
		return preflight.Status
	}
	return domain.ClassifyError(err)
}

// Runner enables a deterministic worker test double without a browser.
type Runner interface {
	Run(context.Context, sources.Adapter, string, bool) (Result, error)
	Preflight(context.Context, sources.Adapter) error
}

type Provider struct {
	RemoteURL string
	Chrome    string
	Input     io.Reader
	Output    io.Writer
}

func New() *Provider {
	return &Provider{
		RemoteURL: strings.TrimSpace(os.Getenv("BROWSERLESS_URL")),
		Chrome:    FindLocalChrome(),
		Input:     os.Stdin,
		Output:    os.Stdout,
	}
}

func FindLocalChrome() string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		}
	default:
		candidates = []string{"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
	}
	for _, candidate := range candidates {
		if candidate != "" {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	for _, executable := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(executable); err == nil {
			return path
		}
	}
	return ""
}

func (p *Provider) Description() string {
	if p.RemoteURL != "" {
		return "Remote CDP: " + redactRemoteURL(p.RemoteURL)
	}
	if p.Chrome != "" {
		return "Local Chrome: " + p.Chrome
	}
	return "未检测到 Chrome，且未配置 BROWSERLESS_URL"
}

func (p *Provider) BrowserVersion(ctx context.Context) (string, error) {
	if p.RemoteURL != "" {
		return "remote CDP configured", nil
	}
	if p.Chrome == "" {
		return "", errors.New("未检测到本机 Chrome/Chromium；安装 Chrome 或设置 BROWSERLESS_URL")
	}
	command := exec.CommandContext(ctx, p.Chrome, "--version")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("读取 Chrome 版本: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ManualReviewError protects users from a remote session that may be visible
// only to Browserless, not the operator who must complete the CAPTCHA.
func (p *Provider) ManualReviewError() error {
	if p.RemoteURL != "" {
		return errors.New("Remote CDP 无法保证本机可见；请取消 BROWSERLESS_URL 后使用 Local Chrome 执行 review")
	}
	return nil
}

func remoteEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("Remote CDP 地址缺少主机")
	}
	switch parsed.Scheme {
	case "ws", "wss":
		return raw, nil
	case "http", "https":
		// Let chromedp discover /json/version. Rewriting this to a websocket
		// endpoint loses CDP providers whose HTTP endpoint is not a websocket.
		return raw, nil
	default:
		return "", fmt.Errorf("Remote CDP 地址必须是 http(s) 或 ws(s)")
	}
}

func discoverRemoteEndpoint(ctx context.Context, raw string) (string, error) {
	endpoint, err := remoteEndpoint(raw)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(endpoint)
	if parsed.Scheme == "ws" || parsed.Scheme == "wss" {
		return endpoint, nil
	}
	discovery := *parsed
	discovery.Path = "/json/version"
	discovery.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Remote CDP discovery 返回 HTTP %d", response.StatusCode)
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil {
		return "", fmt.Errorf("解析 Remote CDP discovery: %w", err)
	}
	discovered, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil || discovered.Host == "" {
		return "", errors.New("Remote CDP discovery 未返回有效 WebSocket 地址")
	}
	if parsed.Scheme == "https" {
		discovered.Scheme = "wss"
	} else {
		discovered.Scheme = "ws"
	}
	discovered.Host = parsed.Host
	discovered.User = parsed.User
	query := discovered.Query()
	for key, values := range parsed.Query() {
		if _, exists := query[key]; !exists {
			query[key] = append([]string(nil), values...)
		}
	}
	discovered.RawQuery = query.Encode()
	return discovered.String(), nil
}

func redactRemoteURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[无效 Remote CDP 地址]"
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		query.Set(key, "REDACTED")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (p *Provider) sanitizeRemoteError(err error) error {
	if err == nil || p.RemoteURL == "" {
		return err
	}
	text := strings.ReplaceAll(err.Error(), p.RemoteURL, redactRemoteURL(p.RemoteURL))
	if parsed, parseErr := url.Parse(p.RemoteURL); parseErr == nil {
		if parsed.User != nil {
			if password, ok := parsed.User.Password(); ok && password != "" {
				text = strings.ReplaceAll(text, password, "REDACTED")
			}
			if username := parsed.User.Username(); username != "" {
				text = strings.ReplaceAll(text, username, "REDACTED")
			}
		}
		for _, values := range parsed.Query() {
			for _, value := range values {
				if value != "" {
					text = strings.ReplaceAll(text, value, "REDACTED")
					text = strings.ReplaceAll(text, url.QueryEscape(value), "REDACTED")
				}
			}
		}
	}
	return errors.New(text)
}

func (p *Provider) context(ctx context.Context, visible bool) (context.Context, context.CancelFunc, error) {
	if p.RemoteURL != "" {
		discoveryCtx, stopDiscovery := context.WithTimeout(ctx, 20*time.Second)
		endpoint, err := discoverRemoteEndpoint(discoveryCtx, p.RemoteURL)
		stopDiscovery()
		if err != nil {
			return nil, nil, p.sanitizeRemoteError(err)
		}
		// Discovery above preserves HTTPS and credentials. Never let chromedp
		// reconstruct the endpoint as plaintext HTTP.
		remoteCtx, cancel := chromedp.NewRemoteAllocator(ctx, endpoint, chromedp.NoModifyURL)
		browserCtx, browserCancel := chromedp.NewContext(remoteCtx)
		return browserCtx, func() { browserCancel(); cancel() }, nil
	}
	if p.Chrome == "" {
		return nil, nil, errors.New("未检测到本机 Chrome/Chromium；运行 legalscout doctor 查看安装说明")
	}
	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(p.Chrome),
		chromedp.Flag("headless", !visible),
		chromedp.Flag("disable-gpu", true),
	)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(ctx, options...)
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	return browserCtx, func() { browserCancel(); allocatorCancel() }, nil
}

func (p *Provider) Preflight(ctx context.Context, source sources.Adapter) error {
	if err := source.PreflightContract(); err != nil {
		return &PreflightError{Status: domain.FatalError, Err: err}
	}
	browserCtx, cancel, err := p.context(ctx, false)
	if err != nil {
		return &PreflightError{Status: domain.ClassifyError(err), Err: err}
	}
	defer cancel()
	timeout, stop := context.WithTimeout(browserCtx, min(source.Timeout, 45*time.Second))
	defer stop()
	if err := chromedp.Run(timeout, navigate(source.URL), waitVisible(source.PreflightSelector)); err != nil {
		err = p.sanitizeRemoteError(err)
		return &PreflightError{Status: domain.ClassifyError(err), Err: fmt.Errorf("%s 预检失败: %w", source.Name, err)}
	}
	return nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (p *Provider) Run(ctx context.Context, source sources.Adapter, subject string, manual bool) (Result, error) {
	if err := source.PreflightContract(); err != nil {
		return Result{Status: domain.FatalError}, err
	}
	if source.RequiresHuman && !manual {
		return Result{Status: domain.NeedsReview}, nil
	}
	if manual {
		if err := p.ManualReviewError(); err != nil {
			return Result{Status: domain.NeedsReview}, err
		}
	}
	var last error
	for attempt := 0; attempt <= source.Retries; attempt++ {
		result, err := p.runOnce(ctx, source, subject, manual)
		if err == nil {
			return result, nil
		}
		err = p.sanitizeRemoteError(err)
		last = err
		// runOnce already separated a source/contract outcome from a
		// transport error. Do not replace that fact with a text heuristic.
		if result.Status == domain.FatalError || result.Status == domain.NeedsReview {
			return result, err
		}
		if !isRetryable(err) {
			return Result{Status: domain.FatalError}, err
		}
	}
	return Result{Status: domain.RetryableError}, last
}

func isRetryable(err error) bool {
	text := strings.ToLower(err.Error())
	return !(strings.Contains(text, "selector") || strings.Contains(text, "contract") || strings.Contains(text, "invalid"))
}

func (p *Provider) runOnce(ctx context.Context, source sources.Adapter, subject string, manual bool) (Result, error) {
	browserCtx, cancel, err := p.context(ctx, manual)
	if err != nil {
		return Result{Status: domain.RetryableError}, err
	}
	defer cancel()
	if source.Selectors.DirectQuery {
		return p.runDirectCSRC(browserCtx, source, subject)
	}
	actionTimeout := source.Timeout
	if manual {
		actionTimeout = 24 * time.Hour
	}
	actionCtx, stopActions := context.WithTimeout(browserCtx, actionTimeout)
	defer stopActions()
	var baseline string
	actions, err := providerActions(source, subject, &baseline)
	if err != nil {
		return Result{Status: domain.FatalError}, err
	}
	if err := chromedp.Run(actionCtx, actions...); err != nil {
		return Result{Status: domain.RetryableError}, fmt.Errorf("查询操作失败: %w", err)
	}
	if manual {
		if p.Output != nil {
			fmt.Fprintln(p.Output, "请在打开的浏览器完成人工验证并确认结果页面，然后按 Enter 继续读取。")
		}
		if p.Input != nil {
			confirmed := make(chan struct{})
			go func() {
				_, _ = bufio.NewReader(p.Input).ReadString('\n')
				close(confirmed)
			}()
			select {
			case <-ctx.Done():
				return Result{Status: domain.RetryableError}, ctx.Err()
			case <-confirmed:
			}
		}
	}
	resultCtx, stopResult := context.WithTimeout(browserCtx, source.Timeout)
	defer stopResult()
	text, png, err := p.readResult(resultCtx, browserCtx, source.Selectors.ResultText, baseline, manual)
	if err != nil {
		return Result{Status: domain.RetryableError}, fmt.Errorf("读取查询结果失败: %w", err)
	}
	status, err := source.Classify(text)
	if err != nil {
		return Result{Status: status}, err
	}
	return Result{Status: status, Screenshot: png}, nil
}

type csrcSearchResponse struct {
	Code int `json:"code"`
	Data *struct {
		Total   *int `json:"total"`
		Results []struct {
			Title            string `json:"title"`
			URL              string `json:"url"`
			PublishedTimeStr string `json:"publishedTimeStr"`
		} `json:"results"`
	} `json:"data"`
}

func decodeCSRCSearch(raw []byte) (csrcSearchResponse, error) {
	var search csrcSearchResponse
	if err := json.Unmarshal(raw, &search); err != nil {
		return search, fmt.Errorf("解析证监会查询结果: %w", err)
	}
	if search.Code != 200 {
		return search, fmt.Errorf("证监会查询接口返回状态 %d", search.Code)
	}
	if search.Data == nil || search.Data.Total == nil || search.Data.Results == nil ||
		*search.Data.Total < 0 || *search.Data.Total < len(search.Data.Results) ||
		(*search.Data.Total > 0 && len(search.Data.Results) == 0) {
		return search, errors.New("证监会查询接口返回不完整结果")
	}
	return search, nil
}

func (p *Provider) runDirectCSRC(browserCtx context.Context, source sources.Adapter, subject string) (Result, error) {
	timeout, stop := context.WithTimeout(browserCtx, source.Timeout)
	defer stop()
	actions := []chromedp.Action{navigate(source.URL), waitVisible(source.Selectors.Input)}
	if source.Selectors.ReadyExpression != "" {
		actions = append(actions, chromedp.Poll(source.Selectors.ReadyExpression, nil))
	}
	for selector, value := range source.Selectors.SetValues {
		actions = append(actions, setValue(selector, value))
	}
	actions = append(actions, setValue(source.Selectors.Input, subject))
	if err := chromedp.Run(timeout, actions...); err != nil {
		return Result{Status: domain.RetryableError}, fmt.Errorf("准备证监会查询页面: %w", err)
	}

	form := url.Values{
		"type": {"title"}, "searchContent": {subject},
		"channelId": {"17d5ff2fe43e488dba825807ae40d63f"},
		"isAgg":     {"true"}, "isIdentifier": {"true"}, "page": {"1"}, "size": {"10"},
	}
	requestExpression := `(async function () {
		const response = await fetch("/getSearch", {
			method: "POST",
			headers: {"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"},
			body: ` + strconv.Quote(form.Encode()) + `
		});
		return {status: response.status, body: await response.text()};
	})()`
	var response struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	awaitPromise := func(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return params.WithAwaitPromise(true)
	}
	if err := chromedp.Run(timeout, chromedp.Evaluate(requestExpression, &response, awaitPromise)); err != nil {
		return Result{Status: domain.RetryableError}, fmt.Errorf("请求证监会查询接口: %w", err)
	}
	if response.Status != 200 {
		return Result{Status: domain.RetryableError}, fmt.Errorf("证监会查询接口返回 HTTP %d", response.Status)
	}
	search, err := decodeCSRCSearch([]byte(response.Body))
	if err != nil {
		return Result{Status: domain.FatalError}, err
	}
	serialized, err := json.Marshal(search)
	if err != nil {
		return Result{Status: domain.FatalError}, err
	}
	expression := `(function () {
		const data = JSON.parse(` + strconv.Quote(string(serialized)) + `);
		const list = document.querySelector("#codeId_list ul");
		if (!list) throw new Error("CSRC result container missing");
		list.replaceChildren();
		if (data.data && data.data.results && data.data.results.length > 0) {
			const table = document.createElement("table");
			const head = document.createElement("tr");
			for (const title of ["序号", "标题", "发文日期"]) {
				const cell = document.createElement("th");
				cell.textContent = title;
				head.appendChild(cell);
			}
			table.appendChild(head);
			data.data.results.forEach(function (result, index) {
				const row = document.createElement("tr");
				const sequence = document.createElement("td");
				sequence.textContent = String(index + 1);
				const title = document.createElement("td");
				const link = document.createElement("a");
				link.textContent = result.title || "";
				link.href = result.url || "#";
				link.target = "_blank";
				title.appendChild(link);
				const date = document.createElement("td");
				date.textContent = (result.publishedTimeStr || "").slice(0, 10);
				row.append(sequence, title, date);
				table.appendChild(row);
			});
			list.appendChild(table);
		} else {
			list.textContent = "抱歉，没找到相关结果。";
		}
	})()`
	var png []byte
	if err := chromedp.Run(timeout, chromedp.Evaluate(expression, nil), chromedp.FullScreenshot(&png, 100)); err != nil {
		return Result{Status: domain.RetryableError}, fmt.Errorf("渲染证监会查询结果: %w", err)
	}
	if len(search.Data.Results) == 0 {
		return Result{Status: domain.NotFound, Screenshot: png}, nil
	}
	return Result{Status: domain.Found, Screenshot: png}, nil
}

type selectorKind string

const (
	selectorCSS   selectorKind = "css"
	selectorXPath selectorKind = "xpath"
)

// normalizeSelector accepts the legacy Playwright xpath= prefix but never
// passes it to chromedp.ByQuery, which only accepts CSS selectors.
func normalizeSelector(raw string) (string, selectorKind, error) {
	selector := strings.TrimSpace(raw)
	if selector == "" {
		return "", "", errors.New("empty selector")
	}
	if strings.HasPrefix(strings.ToLower(selector), "xpath=") {
		selector = strings.TrimSpace(selector[len("xpath="):])
		if selector == "" {
			return "", "", errors.New("empty xpath selector")
		}
		return selector, selectorXPath, nil
	}
	return selector, selectorCSS, nil
}

func selectorOption(kind selectorKind) chromedp.QueryOption {
	if kind == selectorXPath {
		return chromedp.BySearch
	}
	return chromedp.ByQuery
}

func waitVisible(raw string) chromedp.Action {
	selector, kind, err := normalizeSelector(raw)
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error { return err })
	}
	return chromedp.WaitVisible(selector, selectorOption(kind))
}

func click(raw string) chromedp.Action {
	selector, kind, err := normalizeSelector(raw)
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error { return err })
	}
	return chromedp.Click(selector, selectorOption(kind))
}

func setValue(raw, value string) chromedp.Action {
	selector, kind, err := normalizeSelector(raw)
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error { return err })
	}
	return chromedp.SetValue(selector, value, selectorOption(kind))
}

func text(raw string, value *string) chromedp.Action {
	selector, kind, err := normalizeSelector(raw)
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error { return err })
	}
	return chromedp.Text(selector, value, selectorOption(kind))
}

func navigate(raw string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errorText, isDownload, err := cdppage.Navigate(raw).Do(ctx)
		if err != nil {
			return err
		}
		if isDownload {
			return fmt.Errorf("navigation unexpectedly started a download")
		}
		if errorText != "" {
			return fmt.Errorf("navigation failed: %s", errorText)
		}
		return nil
	})
}

type actionStep struct {
	Operation string
	Selector  string
	Kind      selectorKind
}

func providerActionPlan(source sources.Adapter) ([]actionStep, error) {
	steps := make([]actionStep, 0, len(source.Selectors.Before)+len(source.Selectors.SetValues)+4)
	appendStep := func(operation, raw string) error {
		selector, kind, err := normalizeSelector(raw)
		if err != nil {
			return fmt.Errorf("%s selector: %w", operation, err)
		}
		steps = append(steps, actionStep{Operation: operation, Selector: selector, Kind: kind})
		return nil
	}
	if err := appendStep("wait", source.Selectors.Input); err != nil {
		return nil, err
	}
	for _, before := range source.Selectors.Before {
		if err := appendStep("click", before); err != nil {
			return nil, err
		}
	}
	for selector := range source.Selectors.SetValues {
		if err := appendStep("set_value", selector); err != nil {
			return nil, err
		}
	}
	if err := appendStep("set_value", source.Selectors.Input); err != nil {
		return nil, err
	}
	if source.Selectors.Button != "" {
		if err := appendStep("click", source.Selectors.Button); err != nil {
			return nil, err
		}
	}
	if err := appendStep("text", source.Selectors.ResultText); err != nil {
		return nil, err
	}
	return steps, nil
}

func providerActions(source sources.Adapter, subject string, baseline *string) ([]chromedp.Action, error) {
	if _, err := providerActionPlan(source); err != nil {
		return nil, err
	}
	// Use each source selector here rather than the normalized test plan so
	// the action helpers still receive xpath= and select BySearch.
	actions := []chromedp.Action{navigate(source.URL), waitVisible(source.Selectors.Input)}
	if source.Selectors.ReadyExpression != "" {
		actions = append(actions, chromedp.Poll(source.Selectors.ReadyExpression, nil))
	}
	for _, selector := range source.Selectors.Before {
		actions = append(actions, click(selector), chromedp.Sleep(300*time.Millisecond))
	}
	for selector, value := range source.Selectors.SetValues {
		actions = append(actions, setValue(selector, value))
	}
	actions = append(actions, setValue(source.Selectors.Input, subject), optionalText(source.Selectors.ResultText, baseline))
	if source.Selectors.SubmitExpression != "" {
		actions = append(actions, chromedp.Evaluate(source.Selectors.SubmitExpression, nil))
	} else {
		actions = append(actions, click(source.Selectors.Button))
	}
	if source.Selectors.StartedExpression != "" {
		actions = append(actions, chromedp.Poll(source.Selectors.StartedExpression, nil))
	}
	if source.Selectors.CompleteExpression != "" {
		actions = append(actions, chromedp.Poll(source.Selectors.CompleteExpression, nil))
	}
	return actions, nil
}

func optionalText(raw string, value *string) chromedp.Action {
	selector, kind, err := normalizeSelector(raw)
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error { return err })
	}
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var nodes []*cdp.Node
		if err := chromedp.Nodes(selector, &nodes, chromedp.AtLeast(0), selectorOption(kind)).Do(ctx); err != nil {
			return err
		}
		if len(nodes) == 0 {
			*value = ""
			return nil
		}
		return text(raw, value).Do(ctx)
	})
}

func waitForChangedText(raw, baseline string, value *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			var current string
			if err := optionalText(raw, &current).Do(ctx); err != nil {
				return err
			}
			current = strings.TrimSpace(current)
			if current != "" && current != strings.TrimSpace(baseline) {
				*value = current
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
}

func (p *Provider) readResult(timeout, browserCtx context.Context, resultSelector, baseline string, manual bool) (string, []byte, error) {
	contexts := make([]context.Context, 0, 2)
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	if manual && p.RemoteURL == "" {
		targets, err := chromedp.Targets(browserCtx)
		if err == nil {
			for _, target := range targets {
				if target.Type != "page" {
					continue
				}
				targetCtx, cancel := chromedp.NewContext(timeout, chromedp.WithTargetID(target.TargetID))
				contexts = append(contexts, targetCtx)
				cancels = append(cancels, cancel)
			}
		}
	}
	// Check freshly opened local targets before waiting in the original tab.
	// A target probe is intentionally short so an unchanged current tab cannot
	// consume the complete source timeout after the user presses Enter.
	contexts = append(contexts, timeout)
	var last error
	for index, targetCtx := range contexts {
		runCtx := targetCtx
		var stop context.CancelFunc
		if manual && p.RemoteURL == "" && index < len(contexts)-1 {
			runCtx, stop = context.WithTimeout(targetCtx, 2*time.Second)
		}
		var resultText string
		var png []byte
		err := chromedp.Run(runCtx,
			waitVisible(resultSelector),
			waitForChangedText(resultSelector, baseline, &resultText),
			chromedp.FullScreenshot(&png, 100),
		)
		if stop != nil {
			stop()
		}
		if err == nil {
			return resultText, png, nil
		}
		last = err
		select {
		case <-timeout.Done():
			return "", nil, timeout.Err()
		default:
		}
	}
	return "", nil, last
}
