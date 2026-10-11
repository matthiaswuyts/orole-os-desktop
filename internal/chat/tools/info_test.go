package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type infoLLM struct {
	statuses []llm.ProviderStatus
	defaults llm.Defaults
}

func (l infoLLM) Statuses(context.Context) ([]llm.ProviderStatus, error) { return l.statuses, nil }
func (l infoLLM) Defaults(context.Context) (llm.Defaults, error)         { return l.defaults, nil }

type nativeStub struct{}

func (nativeStub) Supports(provider string, _ llm.Caps) bool { return provider == "openai" }
func (nativeStub) Research(context.Context, chat.RunContext, string, string) (Report, error) {
	return Report{}, nil
}

func infoSource(caps llm.Caps, capsErr error) InfoSource {
	return InfoSource{
		LLM: infoLLM{
			statuses: []llm.ProviderStatus{{ID: "openrouter", Name: "OpenRouter"}, {ID: "openai", Name: "OpenAI"}},
			defaults: llm.Defaults{ChatModel: "openrouter/some/model"},
		},
		ModelCaps: func(context.Context, string, string) (llm.Caps, error) { return caps, capsErr },
		Fetcher:   &Fetcher{},
	}
}

func infoByID(t *testing.T, infos []chat.ToolInfo) map[string]chat.ToolInfo {
	t.Helper()
	out := map[string]chat.ToolInfo{}
	for _, i := range infos {
		if _, dup := out[i.ID]; dup {
			t.Fatalf("duplicate tool info %q", i.ID)
		}
		if i.Name == "" || i.Description == "" {
			t.Errorf("%s lacks a name or description: %+v", i.ID, i)
		}
		if i.Available && i.Reason != "" {
			t.Errorf("%s is available but carries a reason: %+v", i.ID, i)
		}
		if !i.Available && i.Reason == "" {
			t.Errorf("%s is unavailable without a reason", i.ID)
		}
		out[i.ID] = i
	}
	return out
}

func describe(t *testing.T, src InfoSource, provider, model string) map[string]chat.ToolInfo {
	t.Helper()
	infos, err := src.Describe(context.Background(), chat.ToolsRequest{Provider: provider, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	return infoByID(t, infos)
}

func TestDescribeHostedEverything(t *testing.T) {
	caps := llm.Caps{Tools: true, HostedTools: []string{llm.HostedWebSearch, llm.HostedWebFetch, llm.HostedDatetime}}
	got := describe(t, infoSource(caps, nil), "openrouter", "m")
	for _, id := range []string{chat.ToolWebSearch, chat.ToolWebFetch, chat.ToolDatetime, chat.ToolResearch} {
		if !got[id].Available {
			t.Errorf("%s unavailable: %+v", id, got[id])
		}
	}
	if e := got[chat.ToolWebSearch].Egress; !strings.Contains(e, "OpenRouter") {
		t.Errorf("search egress = %q, want the provider display name", e)
	}
	if e := got[chat.ToolWebFetch].Egress; !strings.Contains(e, "OpenRouter") {
		t.Errorf("hosted fetch egress = %q", e)
	}
	if got[chat.ToolResearch].Egress == "" {
		t.Error("research must say what leaves the machine")
	}
}

func TestDescribeNoHostedTools(t *testing.T) {
	got := describe(t, infoSource(llm.Caps{Tools: true}, nil), "openrouter", "m")
	if ws := got[chat.ToolWebSearch]; ws.Available || !strings.Contains(ws.Reason, "cannot run hosted web search") {
		t.Errorf("search = %+v", ws)
	}
	if wf := got[chat.ToolWebFetch]; !wf.Available || !strings.Contains(wf.Egress, "this computer") {
		t.Errorf("fetch fallback = %+v", wf)
	}
	if dt := got[chat.ToolDatetime]; !dt.Available {
		t.Errorf("datetime = %+v", dt)
	}
	if rs := got[chat.ToolResearch]; rs.Available || !strings.Contains(rs.Reason, "web search") {
		t.Errorf("research without search = %+v", rs)
	}
}

func TestDescribeModelWithoutTools(t *testing.T) {
	got := describe(t, infoSource(llm.Caps{}, nil), "openrouter", "m")
	for _, id := range []string{chat.ToolWebFetch, chat.ToolDatetime, chat.ToolResearch} {
		if got[id].Available {
			t.Errorf("%s available for a model that cannot call tools: %+v", id, got[id])
		}
	}
}

func TestDescribeFetchFallbackNeedsFetcher(t *testing.T) {
	src := infoSource(llm.Caps{Tools: true}, nil)
	src.Fetcher = nil
	if wf := describe(t, src, "openrouter", "m")[chat.ToolWebFetch]; wf.Available {
		t.Errorf("fetch without a fetcher = %+v", wf)
	}
}

func TestDescribeNativeResearch(t *testing.T) {
	src := infoSource(llm.Caps{Tools: true, DeepResearch: true}, nil)
	src.Native = nativeStub{}
	if rs := describe(t, src, "openai", "m")[chat.ToolResearch]; !rs.Available || !strings.Contains(rs.Egress, "OpenAI") {
		t.Errorf("native research = %+v", rs)
	}
	if rs := describe(t, src, "openrouter", "m")[chat.ToolResearch]; rs.Available {
		t.Errorf("native research for an unsupported provider = %+v", rs)
	}
}

func TestDescribeListsOnlyToggleableTools(t *testing.T) {
	infos, err := infoSource(llm.Caps{Tools: true}, nil).Describe(context.Background(), chat.ToolsRequest{Provider: "openrouter", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 4 {
		t.Fatalf("infos = %+v, want web search, web pages, date and time, research (knowledge is built in)", infos)
	}
}

func TestDescribeCapabilityLookupFailures(t *testing.T) {
	got := describe(t, infoSource(llm.Caps{}, llm.ErrNotConfigured), "openrouter", "m")
	for id, i := range got {
		if i.Available || !strings.Contains(i.Reason, "Connect OpenRouter") {
			t.Errorf("%s = %+v, want a connect-provider reason", id, i)
		}
	}
	got = describe(t, infoSource(llm.Caps{}, errors.New("lookup failed")), "openrouter", "m")
	for id, i := range got {
		if i.Available || !strings.Contains(i.Reason, "unknown") {
			t.Errorf("%s = %+v, want an unknown-capabilities reason", id, i)
		}
	}
}

func TestDescribeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := infoSource(llm.Caps{}, errors.New("interrupted"))
	if _, err := src.Describe(ctx, chat.ToolsRequest{Provider: "openrouter", Model: "m"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDescribeDefaultsAndMissingModel(t *testing.T) {
	var asked struct{ provider, model string }
	src := infoSource(llm.Caps{Tools: true, HostedTools: []string{llm.HostedWebSearch}}, nil)
	src.ModelCaps = func(_ context.Context, p, m string) (llm.Caps, error) {
		asked.provider, asked.model = p, m
		return llm.Caps{Tools: true, HostedTools: []string{llm.HostedWebSearch}}, nil
	}
	got := describe(t, src, "", "")
	if asked.provider != "openrouter" || asked.model != "some/model" {
		t.Fatalf("defaults resolved to %+v", asked)
	}
	if !got[chat.ToolWebSearch].Available {
		t.Errorf("search with the default model = %+v", got[chat.ToolWebSearch])
	}

	src.LLM = infoLLM{}
	for id, i := range describe(t, src, "", "") {
		if i.Available || !strings.Contains(i.Reason, "Choose a model") {
			t.Errorf("%s = %+v, want a choose-a-model reason", id, i)
		}
	}
}
