package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestShowSpecsAreValidSchemasAndNeedToolSupport(t *testing.T) {
	if got := (Show{}).Specs(chat.Bot{}, llm.Caps{}); got != nil {
		t.Fatalf("offered to a model that cannot call tools: %+v", got)
	}
	specs := (Show{}).Specs(chat.Bot{}, llm.Caps{Tools: true})
	if len(specs) != len(showTools) {
		t.Fatalf("specs = %d", len(specs))
	}
	for _, s := range specs {
		var v map[string]any
		if err := json.Unmarshal(s.Schema, &v); err != nil || v["type"] != "object" {
			t.Errorf("%s: schema: %v", s.Name, err)
		}
	}
}

func TestShowEmitsAValidatedUIPart(t *testing.T) {
	cases := []struct{ tool, kind, args string }{
		{ShowTable, "table", `{"columns":[{"key":"n","label":"Name","priority":"primary"},{"key":"p","label":"Price","format":{"kind":"currency","currency":"USD"}}],"rows":[{"n":"A","p":3,"extra":1}]}`},
		{ShowChart, "chart", `{"label":"Runs","value":"9","points":[1,2,9]}`},
		{ShowDiagram, "diagram", `{"title":"t","nodes":[{"id":"a","label":"A","x":10,"y":10},{"id":"b","label":"B","x":200,"y":10}],"links":[{"from":"a","to":"b"}]}`},
		{ShowFlow, "flow", `{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"done"},{"id":"b","label":"B","column":1,"row":0,"state":"active"}],"edges":[{"from":"a","to":"b"}]}`},
		{ShowMath, "math", `{"steps":[{"expression":"x² = 4","note":"square"}]}`},
		{ShowSpecSheet, "spec", `{"title":"Box","rows":[{"label":"Size","value":"4","emphasis":true}]}`},
		{ShowComparison, "comparison", `{"traitLabels":["Fast"],"options":[{"id":"a","name":"A","headline":"h","traits":["yes"]},{"id":"b","name":"B","headline":"h","traits":[null]}],"recommendedId":"a","reason":"r"}`},
		{ShowTimeline, "timeline", `{"events":[{"id":"1","when":"now","time":"09:00","title":"Go"}]}`},
		{ShowJob, "job", `{"title":"Build","stages":[{"name":"a","weight":1}],"stageIndex":0,"stageProgress":0.5}`},
	}
	for _, c := range cases {
		res, err := (Show{}).Call(context.Background(), chat.RunContext{}, c.tool, json.RawMessage(c.args))
		if err != nil || res.IsError || len(res.Parts) != 1 {
			t.Fatalf("%s: %+v %v", c.tool, res, err)
		}
		p := res.Parts[0]
		if p.Type != chat.PartUI || p.Name != c.kind || !json.Valid([]byte(p.Spec)) {
			t.Errorf("%s: part %+v", c.tool, p)
		}
	}
	res, _ := (Show{}).Call(context.Background(), chat.RunContext{}, ShowTable, json.RawMessage(cases[0].args))
	if strings.Contains(res.Parts[0].Spec, "extra") {
		t.Errorf("a value outside the declared columns survived: %s", res.Parts[0].Spec)
	}
}

func TestShowRejectsMalformedComponents(t *testing.T) {
	for name, c := range map[string]struct{ tool, args string }{
		"unknown field":      {ShowChart, `{"label":"a","value":"1","points":[1],"colour":"red"}`},
		"empty series":       {ShowChart, `{"label":"a","value":"1","points":[]}`},
		"bad currency":       {ShowTable, `{"columns":[{"key":"a","label":"A","format":{"kind":"currency","currency":"dollars"}}],"rows":[]}`},
		"duplicate column":   {ShowTable, `{"columns":[{"key":"a","label":"A"},{"key":"a","label":"B"}],"rows":[]}`},
		"object cell":        {ShowTable, `{"columns":[{"key":"a","label":"A"}],"rows":[{"a":{"x":1}}]}`},
		"node off canvas":    {ShowDiagram, `{"title":"t","nodes":[{"id":"a","label":"A","x":590,"y":10}],"links":[]}`},
		"dangling link":      {ShowDiagram, `{"title":"t","nodes":[{"id":"a","label":"A","x":1,"y":1}],"links":[{"from":"a","to":"z"}]}`},
		"dangling edge":      {ShowFlow, `{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"done"}],"edges":[{"from":"a","to":"z"}]}`},
		"unknown recommend":  {ShowComparison, `{"traitLabels":["x"],"options":[{"id":"a","name":"A","headline":"","traits":["1"]},{"id":"b","name":"B","headline":"","traits":["1"]}],"recommendedId":"z","reason":"r"}`},
		"trait count":        {ShowComparison, `{"traitLabels":["x","y"],"options":[{"id":"a","name":"A","headline":"","traits":["1"]},{"id":"b","name":"B","headline":"","traits":["1","2"]}],"recommendedId":"a","reason":"r"}`},
		"stage out of range": {ShowJob, `{"title":"j","stages":[{"name":"a","weight":1}],"stageIndex":2,"stageProgress":0}`},
		"zero weight":        {ShowJob, `{"title":"j","stages":[{"name":"a","weight":0}],"stageIndex":0,"stageProgress":0}`},
		"control characters": {ShowMath, "{\"steps\":[{\"expression\":\"a\\u0007b\"}]}"},
	} {
		res, err := (Show{}).Call(context.Background(), chat.RunContext{}, c.tool, json.RawMessage(c.args))
		if err != nil || !res.IsError || len(res.Parts) != 0 {
			t.Errorf("%s: want an error result, got %+v %v", name, res, err)
		}
	}
}

func TestShowTimelineAcceptsCommonAliasesAndStoresCanonicalNames(t *testing.T) {
	args := `{"title":"Launch","events":[{"date":"2025-09-15","title":"Design freeze","description":"Specs locked","state":"past"},{"time":"Today","title":"Reveal","when":"now"}]}`
	res, err := (Show{}).Call(context.Background(), chat.RunContext{}, ShowTimeline, json.RawMessage(args))
	if err != nil || res.IsError || len(res.Parts) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	var got timelineSpec
	if err := json.Unmarshal([]byte(res.Parts[0].Spec), &got); err != nil {
		t.Fatal(err)
	}
	want := timelineSpec{Title: "Launch", Events: []timelineEvent{
		{ID: "e1", When: "past", Time: "2025-09-15", Title: "Design freeze", Detail: "Specs locked"},
		{ID: "e2", When: "now", Time: "Today", Title: "Reveal"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
