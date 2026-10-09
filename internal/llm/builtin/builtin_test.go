package builtin

import (
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestRegistry(t *testing.T) {
	ds := Descriptors()
	if len(ds) != 2 || ds[0].ID != llm.ProviderOpenRouter || ds[1].ID != llm.ProviderOpenAI {
		t.Fatalf("descriptors = %+v", ds)
	}
	r := Registry()
	for _, d := range ds {
		got, ok := r.Get(d.ID)
		if !ok {
			t.Fatalf("%s not registered", d.ID)
		}
		p, err := got.New(llm.Config{APIKey: "key-0000000000"})
		if err != nil || p.ID() != d.ID {
			t.Fatalf("%s: provider = %v, err = %v", d.ID, p, err)
		}
	}
	if or, _ := r.Get(llm.ProviderOpenRouter); or.OAuth == nil {
		t.Error("openrouter must offer the OAuth sign-in")
	}
	if oa, _ := r.Get(llm.ProviderOpenAI); oa.OAuth != nil {
		t.Error("openai has no OAuth sign-in")
	}
}
