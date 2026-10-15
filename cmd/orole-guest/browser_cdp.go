package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	id    string // target id the tab context is attached to
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

// needAttach decides whether the tab context must be (re)created: there is none, it died, or the visible page target
// is no longer the one attached (the attached tab closed, or the user or a target=_blank click moved to another tab).
// current is "" when Chromium has no page target.
func needAttach(attachedID string, alive bool, current string) bool {
	return !alive || attachedID == "" || attachedID != current
}

// tabContext returns the chromedp context of the visible tab, attaching first when needed. It asks Chromium which
// page is visible on every call, so a closed or replaced tab is noticed even though chromedp keeps the old context
// alive.
func (p *cdpPage) tabContext(ctx context.Context) (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, err := pageTarget(ctx, p.client, p.endpoint)
	if err != nil {
		return nil, err
	}
	if !needAttach(p.id, p.tab != nil && p.tab.Err() == nil, id) {
		return p.tab, nil
	}
	// The old context is only dropped, never cancelled: with a remote allocator cancelling a live tab context
	// closes that tab in the user's visible Chromium. A context whose tab is gone is already dead.
	p.tab, p.id = nil, ""
	if p.alloc == nil {
		// The path-less ws:// URL makes chromedp look up the browser's websocket address on /json/version.
		p.alloc, _ = chromedp.NewRemoteAllocator(context.Background(), strings.Replace(p.endpoint, "http://", "ws://", 1)+"/")
	}
	var opts []chromedp.ContextOption
	if id != "" {
		opts = append(opts, chromedp.WithTargetID(target.ID(id)))
	}
	// stop must never be called on a live tab: it would close the user's visible tab. It is only used below, when the
	// attach itself failed and nothing is attached.
	tab, stop := chromedp.NewContext(p.alloc, opts...)
	attached := make(chan error, 1)
	go func() { attached <- chromedp.Run(tab) }()
	select {
	case err := <-attached:
		if err != nil {
			stop()
			return nil, fmt.Errorf("%w: %v", guestagent.ErrNoBrowser, err)
		}
	case <-ctx.Done():
		// Bound by the request, not by a deadline on the tab. Let the attach finish in the background; if it
		// failed there is nothing attached, so releasing it is safe, otherwise it is simply abandoned.
		go func() {
			if <-attached != nil {
				stop()
			}
		}()
		return nil, ctx.Err()
	}
	p.tab = tab
	p.id = string(chromedp.FromContext(tab).Target.TargetID)
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
		if tab.Err() != nil { // the connection to Chromium was lost mid-action
			return fmt.Errorf("%w: %v", guestagent.ErrNoBrowser, err)
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

// resolve marks the element target names with a fresh random marker and returns a selector for it.
func (p *cdpPage) resolve(ctx context.Context, target string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	marker := hex.EncodeToString(raw[:])
	var found bool
	if err := p.run(ctx, chromedp.Evaluate(resolveScript(marker, target), &found)); err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("nothing visible on the page matches %q", target)
	}
	return `[data-orole-target="` + marker + `"]`, nil
}

// resolveJS finds a visible element by CSS selector, or by its visible text (exact, then partial match), marks it
// with data-orole-target=<marker> and scrolls it into view. Its arguments are the marker and target (JSON strings) and a boolean.
const resolveJS = `(function (marker, target, byText) {
  document.querySelectorAll('[data-orole-target]').forEach(function (e) { e.removeAttribute('data-orole-target'); });
  function visible(el) { var r = el.getBoundingClientRect(); var s = getComputedStyle(el); return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; }
  var el = null;
  if (!byText) { try { el = document.querySelector(target); } catch (e) { el = null; } if (el && !visible(el)) el = null; }
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
  el.setAttribute('data-orole-target', marker);
  el.scrollIntoView({block: 'center', inline: 'center'});
  return true;
})(%s, %s, %t)`

// resolveScript builds resolveJS for target. wire.ParseTarget decides, exactly as request validation does, whether
// the target is matched by visible text ("text=" prefix) or as a CSS selector (falling back to text if no match).
func resolveScript(marker, target string) string {
	value, byText := wire.ParseTarget(target)
	quoted, _ := json.Marshal(value) // also escapes U+2028/U+2029
	quotedMarker, _ := json.Marshal(marker)
	return fmt.Sprintf(resolveJS, quotedMarker, quoted, byText)
}
