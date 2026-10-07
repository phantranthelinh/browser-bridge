package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgSummaryMarksOptionalArgsAndChoices(t *testing.T) {
	cases := map[string]string{
		"navigate":  "`url`, `newTab?`, `groupTitle?`",
		"select":    "`selector`, `value`/`label`",
		"wait_for":  "`selector`/`text`/`urlContains`/`load`, `state?`",
		"scroll":    "`selector`/`direction`, `amount?`",
		"list_tabs": "",
	}
	for name, want := range cases {
		a, _ := Lookup(name)
		if got := argSummary(a); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}

func TestFlatInputSchemasAreOneObjectEach(t *testing.T) {
	for _, a := range Actions {
		var s map[string]any
		if err := json.Unmarshal(FlatInputSchema(a), &s); err != nil {
			t.Fatal(err)
		}
		if s["type"] != "object" || s["properties"] == nil {
			t.Errorf("%s: %v", a.Name, s)
		}
		raw := string(FlatInputSchema(a))
		for _, kw := range []string{`"oneOf"`, `"dependentRequired"`, `"const"`} {
			if strings.Contains(raw, kw) {
				t.Errorf("%s still has %s: %s", a.Name, kw, raw)
			}
		}
	}
	// The full schema keeps them: the daemon validates with it.
	a, _ := Lookup("wait_for")
	if !strings.Contains(string(InputSchema(a)), `"oneOf"`) {
		t.Error("InputSchema lost oneOf")
	}
}

func TestWithToolTableReplacesOnlyBetweenTheMarkers(t *testing.T) {
	doc := "# Skill\n\n<!-- tools:begin -->\nold table\n<!-- tools:end -->\n\nafter\n"
	got, err := WithToolTable([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Skill\n\n<!-- tools:begin -->\n" + ToolTable() + "<!-- tools:end -->\n\nafter\n"
	if string(got) != want {
		t.Fatalf("got:\n%s", got)
	}
	if !strings.Contains(ToolTable(), "| `click` | `selector` | Click an element with a real mouse event. |") {
		t.Errorf("table:\n%s", ToolTable())
	}
	if _, err := WithToolTable([]byte("no markers")); err == nil {
		t.Error("a document without markers must fail")
	}
}
