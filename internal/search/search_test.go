package search

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueryMergesAndReportsProviderErrors(t *testing.T) {
	a := New()
	a.Timeout = 100 * time.Millisecond
	a.Register("a", func(ctx context.Context, q string, limit int) ([]Result, error) {
		return []Result{{ID: "1", Title: "alpha", Score: 10}, {ID: "2", Title: "beta", Score: 50}}, nil
	})
	a.Register("b", func(ctx context.Context, q string, limit int) ([]Result, error) {
		return nil, errors.New("boom")
	})
	a.Register("slow", func(ctx context.Context, q string, limit int) ([]Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	r := a.Query(context.Background(), "x", nil, 10)
	if len(r.Results) != 2 || r.Results[0].ID != "2" || r.Results[0].Provider != "a" {
		t.Fatalf("results: %+v", r.Results)
	}
	if r.Errors["b"] != "boom" || r.Errors["slow"] == "" {
		t.Fatalf("errors: %+v", r.Errors)
	}

	r = a.Query(context.Background(), "x", []string{"a", "nope"}, 1)
	if len(r.Results) != 1 || r.Errors["nope"] == "" || r.Errors["a"] != "" {
		t.Fatalf("subset: %+v", r)
	}
	if r := a.Query(context.Background(), "   ", nil, 5); len(r.Results) != 0 || r.Errors != nil {
		t.Fatalf("blank: %+v", r)
	}
}

func TestFuzzyOrdering(t *testing.T) {
	exact, prefix, word, sub, subseq := Fuzzy("ab", "ab"), Fuzzy("ab", "abc"), Fuzzy("ab", "x ab"), Fuzzy("ab", "xxab"), Fuzzy("ab", "a-x-b")
	if !(exact > prefix && prefix > word && word > sub && sub > subseq && subseq > 0) {
		t.Fatalf("%v %v %v %v %v", exact, prefix, word, sub, subseq)
	}
	if Fuzzy("zz", "abc") != 0 {
		t.Fatal("non-match scored")
	}
}
