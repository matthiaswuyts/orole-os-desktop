package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/guestagent"
	"github.com/rayorole/orole-os-desktop/internal/sandbox/wire"
)

// cdpEndpoint is Chromium's DevTools endpoint inside the guest (loopback only; the guest image starts Chromium with
// --remote-debugging-address=127.0.0.1 --remote-debugging-port=9222).
const cdpEndpoint = "http://127.0.0.1:9222"

// cdpPage drives the visible Chromium tab the user watches. It attaches to the first page target and keeps that
// attachment; a tab that went away is attached again (or a new one opened) on the next action.
type cdpPage struct {
	endpoint string
	client   *http.Client

	mu    sync.Mutex
	alloc context.Context
	tab   context.Context
	stop  context.CancelFunc
}

var _ guestagent.BrowserPage = (*cdpPage)(nil)

func newCDPPage(endpoint string) *cdpPage {
	return &cdpPage{endpoint: endpoint, client: &http.Client{Timeout: 5 * time.Second}}
}

// cdpTarget is one entry of Chromium's /json/list.
type cdpTarget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// pageTarget returns the id of the first page target (the visible tab), or "" when Chromium has none. An endpoint
// that cannot be reached is guestagent.ErrNoBrowser.
func pageTarget(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/json/list", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", guestagent.ErrNoBrowser, err)
	}
	defer resp.Body.Close()
	var list []cdpTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return "", fmt.Errorf("%w: %v", guestagent.ErrNoBrowser, err)
	}
	for _, t := range list {
		if t.Type == "page" {
			return t.ID, nil
		}
	}
	return "", nil
}

// tabContext returns the chromedp context of the visible tab, attaching first when needed.
func (p *cdpPage) tabContext(ctx context.Context) (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tab != nil && p.tab.Err() == nil {
		return p.tab, nil
	}
	if p.stop != nil {
		p.stop() // release the dead attachment
		p.tab, p.stop = nil, nil
	}
	if p.alloc == nil {
		// The path-less ws:// URL makes chromedp look up the browser's websocket address on /json/version.
		p.alloc, _ = chromedp.NewRemoteAllocator(context.Background(), strings.Replace(p.endpoint, "http://", "ws://", 1)+"/")
	}
	id, err := pageTarget(ctx, p.client, p.endpoint)
	if err != nil {
		return nil, err
	}
	var opts []chromedp.ContextOption
	if id != "" {
		opts = append(opts, chromedp.WithTargetID(target.ID(id)))
	}
	tab, stop := chromedp.NewContext(p.alloc, opts...)
	if err := chromedp.Run(tab); err != nil {
		stop()
		return nil, fmt.Errorf("%w: %v", guestagent.ErrNoBrowser, err)
	}
	p.tab, p.stop = tab, stop
	return tab, nil
}

// run executes actions on the tab, bounded by ctx without ever cancelling the tab itself.
func (p *cdpPage) run(ctx context.Context, actions ...chromedp.Action) error {
	tab, err := p.tabContext(ctx)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(tab)
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	if err := chromedp.Run(runCtx, actions...); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (p *cdpPage) Navigate(ctx context.Context, url string) error {
	return p.run(ctx, chromedp.Navigate(url))
}

func (p *cdpPage) Click(ctx context.Context, target string) error {
	sel, err := p.resolve(ctx, target)
	if err != nil {
		return err
	}
	return p.run(ctx, chromedp.Click(sel, chromedp.ByQuery, chromedp.NodeVisible))
}

func (p *cdpPage) Type(ctx context.Context, target, text string) error {
	sel, err := p.resolve(ctx, target)
	if err != nil {
		return err
	}
	return p.run(ctx, chromedp.Click(sel, chromedp.ByQuery, chromedp.NodeVisible), chromedp.SendKeys(sel, text, chromedp.ByQuery))
}

func (p *cdpPage) Info(ctx context.Context) (string, string, error) {
	var title, url string
	err := p.run(ctx, chromedp.Title(&title), chromedp.Location(&url))
	return title, url, err
}

func (p *cdpPage) Text(ctx context.Context) (string, error) {
	var text string
	err := p.run(ctx, chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &text))
	return text, err
}

func (p *cdpPage) Screenshot(ctx context.Context) ([]byte, error) {
	var png []byte
	err := p.run(ctx, chromedp.CaptureScreenshot(&png))
	return png, err
}

// markedTarget is the selector of the element resolveScript marked.
const markedTarget = `[data-orole-target="1"]`

// resolve marks the element target names and returns a selector for it.
func (p *cdpPage) resolve(ctx context.Context, target string) (string, error) {
	var found bool
	if err := p.run(ctx, chromedp.Evaluate(resolveScript(target), &found)); err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("nothing on the page matches %q", target)
	}
	return markedTarget, nil
}

// resolveJS finds a visible element by CSS selector, or by its visible text (exact, then partial match), marks it
// with data-orole-target="1" and scrolls it into view. Its two arguments are a JSON string and a boolean.
const resolveJS = `(function (target, byText) {
  document.querySelectorAll('[data-orole-target]').forEach(function (e) { e.removeAttribute('data-orole-target'); });
  function visible(el) { var r = el.getBoundingClientRect(); var s = getComputedStyle(el); return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; }
  var el = null;
  if (!byText) { try { el = document.querySelector(target); } catch (e) { el = null; } }
  if (!el) {
    var want = target.trim().toLowerCase(), partial = null;
    var nodes = document.querySelectorAll('a,button,input,textarea,select,label,summary,[role],[onclick],[contenteditable]');
    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      if (!visible(n)) continue;
      var label = (n.innerText || n.value || n.getAttribute('aria-label') || n.getAttribute('placeholder') || '').trim().toLowerCase();
      if (label === want) { el = n; break; }
      if (!partial && label && label.indexOf(want) >= 0) partial = n;
    }
    el = el || partial;
  }
  if (!el) return false;
  el.setAttribute('data-orole-target', '1');
  el.scrollIntoView({block: 'center', inline: 'center'});
  return true;
})(%s, %t)`

// resolveScript builds resolveJS for target. wire.ParseTarget decides, exactly as request validation does, whether
// the target is matched by visible text ("text=" prefix) or as a CSS selector (falling back to text if no match).
func resolveScript(target string) string {
	value, byText := wire.ParseTarget(target)
	quoted, _ := json.Marshal(value) // also escapes U+2028/U+2029
	return fmt.Sprintf(resolveJS, quoted, byText)
}
