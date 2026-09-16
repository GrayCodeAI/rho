package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// Shared headless browser. The exec allocator is created lazily on first use
// and reused across calls so repeated navigations do not pay a Chrome startup
// cost. All access must go through acquireBrowser/releaseBrowser so the mutex
// guards the allocator lifecycle.
var (
	browserMu        sync.Mutex
	browserAllocator *execBrowser
)

// execBrowser wraps the chromedp exec allocator context and its cancel func.
type execBrowser struct {
	ctx         context.Context
	cancel      context.CancelFunc // cancels the browser context
	cancelAlloc context.CancelFunc // cancels the exec allocator (kills Chrome)
}

// acquireBrowser returns the chromedp context built on the shared allocator,
// creating the allocator on first use.
func acquireBrowser() (context.Context, error) {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserAllocator == nil {
		abctx, cancelAlloc := chromedp.NewExecAllocator(
			context.Background(),
			chromedp.Headless,
			chromedp.NoSandbox,
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.DisableGPU,
			chromedp.WindowSize(1280, 800),
		)
		ebctx, cancelBrowser := chromedp.NewContext(abctx)
		browserAllocator = &execBrowser{
			ctx:         ebctx,
			cancel:      cancelBrowser,
			cancelAlloc: cancelAlloc,
		}
	}
	return browserAllocator.ctx, nil
}

// releaseBrowser tears down the shared browser so its Chrome process exits.
// Safe to call multiple times; no-op when nothing is running.
func releaseBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserAllocator != nil {
		browserAllocator.cancel()
		browserAllocator.cancelAlloc()
		browserAllocator = nil
	}
}

// missingChromeHint is returned when a headless browser binary cannot be found.
const missingChromeHint = "no Chrome/Chromium executable found; install Chrome or set CHROME_PATH"

// browserErr wraps raw chromedp failures with the missing-browser hint when
// the failure happens during allocator startup.
func browserErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "exec:") || strings.Contains(err.Error(), "executable file not found") {
		return fmt.Errorf("%s: %w", missingChromeHint, err)
	}
	return err
}

// validateBrowserURL allows http/https (including localhost and LAN dev
// servers, which are common screenshot targets) but blocks schemes that would
// reach into local files or privileged endpoints.
func validateBrowserURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("only http and https urls are supported, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("url must include a host")
	}
	return nil
}

// BrowserTool drives a headless Chrome browser through the Chrome DevTools
// Protocol. It covers browser automation (navigate, click, type) and content
// extraction (title, location, text/html), and can persist full-page
// screenshots for visual verification.
type BrowserTool struct{}

func (BrowserTool) Name() string      { return "Browser" }
func (BrowserTool) Aliases() []string { return []string{"browser"} }
func (BrowserTool) RiskLevel() string { return "high" }
func (BrowserTool) Description() string {
	return "Control a headless Chrome browser: navigate to URLs, click and type into elements, extract page text/HTML/title, and take screenshots."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (BrowserTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":    {Type: "string", Enum: []interface{}{"navigate", "content", "screenshot", "click", "type", "title", "location", "ax_snapshot", "close"}, Description: "Actions. navigate/content/screenshot/title/location as named. ax_snapshot: compressed accessibility tree with uid handles (query optional); click/type then accept uid from that snapshot instead of a CSS selector. close shuts the browser down."},
			"url":       {Type: "string", Description: "Target URL (http/https) for navigate/screenshot"},
			"selector":  {Type: "string", Description: "CSS selector for content/click/type and optional navigate wait"},
			"uid":       {Type: "string", Description: "Element uid from the last ax_snapshot; preferred over selector for click/type"},
			"text":      {Type: "string", Description: "Text to type (type action)"},
			"clear":     {Type: "boolean", Description: "Clear the field before typing"},
			"path":      {Type: "string", Description: "File path to save a screenshot to"},
			"wait_ms":   {Type: "number", Description: "Milliseconds to wait after navigation (default 800)"},
			"html":      {Type: "boolean", Description: "Return outer HTML instead of inner text (content action)"},
			"max_chars": {Type: "number", Description: "Truncate extracted content to this many characters (default 20000)"},
		},
		Required: []string{"action"},
	}
}

func (BrowserTool) Parameters() map[string]interface{} {
	return browserSchema.ToJSONSchema()
}

// browserSchema is the single source of truth for Browser's input schema.
var browserSchema = BrowserTool{}.Schema()

// BrowserInput is the typed input for BrowserTool.
type BrowserInput struct {
	Action   string `json:"action"`
	URL      string `json:"url"`
	Selector string `json:"selector"`
	UID      string `json:"uid"`
	Text     string `json:"text"`
	Clear    bool   `json:"clear"`
	Path     string `json:"path"`
	WaitMS   int    `json:"wait_ms"`
	HTML     bool   `json:"html"`
	MaxChars int    `json:"max_chars"`
}

func (BrowserTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[BrowserInput]("Browser", input)
	if err != nil {
		return "", err
	}
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	if p.Action == "" {
		return "", fmt.Errorf("action is required")
	}

	wait := time.Duration(p.WaitMS) * time.Millisecond
	if wait <= 0 {
		wait = 800 * time.Millisecond
	}
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 20000
	}

	if p.Action == "close" {
		releaseBrowser()
		return "Browser closed.", nil
	}

	if err := validateBrowserURL(p.URL); err != nil {
		return "", err
	}

	bctx, err := acquireBrowser()
	if err != nil {
		return "", browserErr(err)
	}

	var out string

	switch p.Action {
	case "navigate":
		actions := []chromedp.Action{
			chromedp.Navigate(p.URL),
			chromedp.Sleep(wait),
		}
		if p.Selector != "" {
			sel := p.Selector
			actions = append(actions, chromedp.ActionFunc(func(c context.Context) error {
				return waitForSelector(c, sel, 10*time.Second)
			}))
		}
		var loc string
		actions = append(actions, chromedp.Location(&loc))
		if err := chromedp.Run(bctx, actions...); err != nil {
			return "", browserErr(err)
		}
		out = fmt.Sprintf("Navigated to %s", loc)

	case "title":
		var title string
		if err := chromedp.Run(bctx, chromedp.Navigate(p.URL), chromedp.Sleep(wait), chromedp.Title(&title)); err != nil {
			return "", browserErr(err)
		}
		out = title

	case "content":
		var text string
		var contentAction chromedp.Action
		if p.Selector != "" {
			sel := p.Selector
			if p.HTML {
				contentAction = chromedp.OuterHTML(sel, &text, chromedp.ByQuery)
			} else {
				contentAction = chromedp.Text(sel, &text, chromedp.ByQuery)
			}
		} else if p.HTML {
			contentAction = chromedp.OuterHTML("html", &text, chromedp.ByQuery)
		} else {
			contentAction = chromedp.Evaluate(`document.body.innerText`, &text)
		}
		if err := chromedp.Run(bctx, chromedp.Navigate(p.URL), chromedp.Sleep(wait), contentAction); err != nil {
			return "", browserErr(err)
		}
		out = truncateChars(text, maxChars)

	case "screenshot":
		dest := p.Path
		if dest == "" {
			dest = filepath.Join(os.TempDir(), fmt.Sprintf("rho-browser-%d.png", time.Now().UnixNano()))
		}
		if err := validatePathAllowed(ctx, dest); err != nil {
			return "", err
		}
		buf, err := captureFullScreenshot(p.URL, p.Selector, wait, 0, 0)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, buf, 0o644); err != nil {
			return "", fmt.Errorf("write screenshot: %w", err)
		}
		out = fmt.Sprintf("Screenshot saved to %s (%d bytes)", dest, len(buf))

	case "click":
		if ref, ok := lookupUID(p.UID); ok {
			if err := actByBackendID(bctx, ref.BackendDOMID, axClickJS); err != nil {
				return "", browserErr(err)
			}
			out = fmt.Sprintf("Clicked %s %q via uid %s (settled:false — re-snapshot to confirm)", ref.Role, ref.Name, ref.UID)
			break
		}
		if p.Selector == "" {
			return "", fmt.Errorf("click requires uid (from ax_snapshot) or selector")
		}
		if err := chromedp.Run(bctx, chromedp.Navigate(p.URL), chromedp.Sleep(wait), chromedp.Click(p.Selector, chromedp.ByQuery)); err != nil {
			return "", browserErr(err)
		}
		out = fmt.Sprintf("Clicked %s", p.Selector)

	case "ax_snapshot":
		out, err = axSnapshot(bctx, p.Text)
		if err != nil {
			return "", err
		}

	case "type":
		if p.Selector == "" && p.UID == "" {
			return "", fmt.Errorf("type requires uid (from ax_snapshot) or selector")
		}
		if p.Selector == "" {
			ref, ok := lookupUID(p.UID)
			if !ok {
				return "", fmt.Errorf("unknown uid %q — take a fresh ax_snapshot", p.UID)
			}
			if err := actByBackendID(bctx, ref.BackendDOMID, axTypeWrapJS(p.Text)); err != nil {
				return "", browserErr(err)
			}
			out = fmt.Sprintf("Typed %d characters into %s %q (uid %s)", len(p.Text), ref.Role, ref.Name, ref.UID)
			break
		}
		sel := p.Selector
		actions := []chromedp.Action{chromedp.Navigate(p.URL), chromedp.Sleep(wait)}
		if p.Clear {
			actions = append(actions, chromedp.Clear(sel, chromedp.ByQuery))
		}
		actions = append(actions, chromedp.SendKeys(sel, p.Text, chromedp.ByQuery))
		if err := chromedp.Run(bctx, actions...); err != nil {
			return "", browserErr(err)
		}
		out = fmt.Sprintf("Typed %d characters into %s", len(p.Text), sel)

	case "location":
		var loc string
		if err := chromedp.Run(bctx, chromedp.Location(&loc)); err != nil {
			return "", browserErr(err)
		}
		out = loc

	default:
		return "", fmt.Errorf("unknown browser action %q", p.Action)
	}

	return out, nil
}

// truncateChars trims s to at most max runes with an ellipsis suffix.
func truncateChars(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// captureFullScreenshot drives a headless browser to navigate to url (optionally
// waiting for a selector) and return a full-page PNG. The viewport is set to
// width x height, with 0 values falling back to the allocator's default
// (1280x800). Shared by BrowserTool's screenshot action and ScreenshotTool to
// avoid drift.
func captureFullScreenshot(url, selector string, wait time.Duration, width, height int) ([]byte, error) {
	if err := validateBrowserURL(url); err != nil {
		return nil, err
	}
	bctx, err := acquireBrowser()
	if err != nil {
		return nil, browserErr(err)
	}
	actions := []chromedp.Action{}
	if width > 0 && height > 0 {
		actions = append(actions, chromedp.EmulateViewport(int64(width), int64(height)))
	}
	actions = append(
		actions,
		chromedp.Navigate(url),
		chromedp.Sleep(wait),
	)
	if selector != "" {
		sel := selector
		actions = append(actions, chromedp.ActionFunc(func(c context.Context) error {
			return waitForSelector(c, sel, 10*time.Second)
		}))
	}
	var buf []byte
	actions = append(actions, chromedp.FullScreenshot(&buf, 100))
	if err := chromedp.Run(bctx, actions...); err != nil {
		return nil, browserErr(err)
	}
	return buf, nil
}

// waitForSelector polls document.querySelector via the already-running page
// until the selector matches or the timeout elapses.
func waitForSelector(ctx context.Context, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		expr := fmt.Sprintf("!!document.querySelector(%q)", selector)
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err != nil {
			return fmt.Errorf("wait for selector %q: %w", selector, err)
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for selector %q", selector)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
