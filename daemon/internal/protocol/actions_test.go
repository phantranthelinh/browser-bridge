package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryActionHasUniqueNameAndTimeout(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Actions {
		if seen[a.Name] {
			t.Errorf("duplicate action %s", a.Name)
		}
		seen[a.Name] = true
		if a.DefaultTimeoutMs <= 0 || a.DefaultTimeoutMs > MaxTimeoutMs {
			t.Errorf("%s: bad default timeout %d", a.Name, a.DefaultTimeoutMs)
		}
		if a.Description == "" {
			t.Errorf("%s: missing description", a.Name)
		}
	}
	for _, name := range []string{"navigate", "reload", "go_back", "go_forward"} {
		if a, _ := Lookup(name); a.DefaultTimeoutMs != 30000 {
			t.Errorf("%s: want 30000, got %d", name, a.DefaultTimeoutMs)
		}
	}
	if a, _ := Lookup("click"); a.DefaultTimeoutMs != 15000 {
		t.Errorf("click: want 15000, got %d", a.DefaultTimeoutMs)
	}
	if _, ok := Lookup("hover"); ok {
		t.Error("hover is not an MVP action")
	}
}

func TestValidateArgs(t *testing.T) {
	cases := []struct {
		action, args string
		ok           bool
		errContains  string
	}{
		{"navigate", `{"url":"https://a.com"}`, true, ""},
		{"navigate", `{}`, false, "url"},
		{"navigate", `{"url":"https://a.com","bogus":1}`, false, "bogus"},
		{"list_tabs", ``, true, ""},
		{"list_tabs", `null`, true, ""},
		{"list_tabs", `{"x":1}`, false, "x"},
		{"fill", `{"selector":"@e1","value":""}`, true, ""},
		{"fill", `{"selector":"@e1"}`, false, "value"},
		{"click", `{"selector":""}`, false, "selector"},
		{"select", `{"selector":"#s","value":"a"}`, true, ""},
		{"select", `{"selector":"#s","label":"A"}`, true, ""},
		{"select", `{"selector":"#s","value":"a","label":"A"}`, false, ""},
		{"select", `{"selector":"#s"}`, false, ""},
		{"scroll", `{"direction":"down","amount":300}`, true, ""},
		{"scroll", `{"selector":"@e1"}`, true, ""},
		{"scroll", `{"amount":300}`, false, ""},
		{"scroll", `{"selector":"@e1","direction":"down"}`, false, ""},
		{"scroll", `{"direction":"sideways"}`, false, "direction"},
		{"wait_for", `{"selector":"#x","state":"hidden"}`, true, ""},
		{"wait_for", `{"text":"Done"}`, true, ""},
		{"wait_for", `{"load":true}`, true, ""},
		{"wait_for", `{"load":false}`, false, "load"},
		{"wait_for", `{"text":"a","urlContains":"b"}`, false, ""},
		{"wait_for", `{"text":"a","state":"hidden"}`, false, "state"},
		{"wait_for", `{}`, false, ""},
		{"screenshot", `{}`, true, ""},
		{"screenshot", `{"format":"jpeg","quality":101}`, false, "quality"},
		{"screenshot", `{"format":"gif"}`, false, "format"},
		{"upload", `{"selector":"#f","files":[]}`, false, "files"},
		{"handle_dialog", `{"accept":true}`, true, ""},
		{"handle_dialog", `{}`, false, "accept"},
		{"cdp", `{"method":"DOM.getDocument","params":{"depth":1}}`, true, ""},
		{"snapshot", `{"maxChars":0}`, false, "maxChars"},
		{"navigate", `[1,2]`, false, ""},
		{"navigate", `{"url":`, false, "JSON"},
	}
	for _, c := range cases {
		a, ok := Lookup(c.action)
		if !ok {
			t.Fatalf("unknown action %s", c.action)
		}
		err := ValidateArgs(a, json.RawMessage(c.args))
		if c.ok && err != nil {
			t.Errorf("%s %s: unexpected error %v", c.action, c.args, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s %s: expected an error", c.action, c.args)
		}
		if err != nil && c.errContains != "" && !strings.Contains(err.Error(), c.errContains) {
			t.Errorf("%s %s: error %q should mention %q", c.action, c.args, err, c.errContains)
		}
	}
}

func TestValidationErrorHidesSchemaURL(t *testing.T) {
	a, _ := Lookup("navigate")
	err := ValidateArgs(a, json.RawMessage(`{}`))
	if strings.Contains(err.Error(), "mem:///") {
		t.Fatalf("error leaks schema url: %v", err)
	}
}

func TestDocumentHasEveryActionType(t *testing.T) {
	b, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"NavigateArgs", "TabResult", "WaitForArgs", "ScreenshotCapture", "Hello", "Welcome", "RequestFrame", "ResponseFrame", "Error", "ActionName"} {
		if _, ok := doc.Defs[name]; !ok {
			t.Errorf("$defs is missing %s", name)
		}
	}
	if !strings.Contains(string(doc.Defs["Welcome"]), "blockedHosts") {
		t.Error("Welcome must carry blockedHosts")
	}
}

func TestFramesPinProtocolVersion(t *testing.T) {
	b, _ := Document()
	var doc struct {
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	json.Unmarshal(b, &doc)
	for _, name := range []string{"Hello", "Welcome"} {
		if got := doc.Defs[name].Properties["protocolVersion"]["const"]; got != float64(ProtocolVersion) {
			t.Errorf("%s.protocolVersion const = %v, want %d", name, got, ProtocolVersion)
		}
	}
}
