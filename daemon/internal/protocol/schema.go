package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/invopop/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaFor reflects a Go value into an inline JSON Schema with no $schema, $id or $ref, so it can
// be embedded as a tool's inputSchema or as one entry of $defs.
func SchemaFor(v any) *jsonschema.Schema {
	r := &jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	s := r.Reflect(v)
	s.Version = ""
	s.ID = ""
	return s
}

// InputSchema is the JSON Schema of an action's args.
func InputSchema(a Action) json.RawMessage {
	b, err := json.Marshal(SchemaFor(a.Args))
	if err != nil {
		panic(err)
	}
	return b
}

// frameTypes are the non-action types the extension needs TypeScript definitions for.
var frameTypes = []any{
	Hello{}, Welcome{}, RequestFrame{}, ResponseFrame{}, EventFrame{}, PingFrame{}, PongFrame{},
	Error{}, ScreenshotCapture{},
}

// Document renders schema/protocol.schema.json: one $defs entry per named Go type, keyed by the
// type name, so json-schema-to-typescript emits interfaces with the same names as the Go structs.
func Document() ([]byte, error) {
	defs := map[string]*jsonschema.Schema{}
	add := func(v any) {
		name := reflect.TypeOf(v).Name()
		if name == "" {
			return
		}
		s := SchemaFor(v)
		s.Title = name
		defs[name] = s
	}
	actionNames := make([]string, 0, len(Actions))
	for _, a := range Actions {
		add(a.Args)
		add(a.Result)
		actionNames = append(actionNames, a.Name)
	}
	for _, v := range frameTypes {
		add(v)
	}
	defs["ActionName"] = &jsonschema.Schema{Title: "ActionName", Type: "string", Enum: toAny(actionNames)}

	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "BrowserBridgeProtocol",
		"$defs":   defs,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

var (
	compileOnce sync.Once
	compiled    map[string]*validator.Schema
)

func compileAll() {
	compiled = map[string]*validator.Schema{}
	c := validator.NewCompiler()
	for _, a := range Actions {
		doc, err := validator.UnmarshalJSON(bytes.NewReader(InputSchema(a)))
		if err != nil {
			panic(err)
		}
		url := "mem:///" + a.Name + ".json"
		if err := c.AddResource(url, doc); err != nil {
			panic(err)
		}
		s, err := c.Compile(url)
		if err != nil {
			panic(fmt.Sprintf("schema for %s: %v", a.Name, err))
		}
		compiled[a.Name] = s
	}
}

// ValidateArgs checks args against the action's schema. Missing args count as {}. The returned
// error message is meant for the agent, so it names the offending field and nothing else.
func ValidateArgs(a Action, args json.RawMessage) error {
	compileOnce.Do(compileAll)
	if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
		args = json.RawMessage("{}")
	}
	inst, err := validator.UnmarshalJSON(bytes.NewReader(args))
	if err != nil {
		return fmt.Errorf("args is not valid JSON: %v", err)
	}
	err = compiled[a.Name].Validate(inst)
	if err == nil {
		return nil
	}
	ve, ok := err.(*validator.ValidationError)
	if !ok {
		return err
	}
	// The first line names the schema URL, which means nothing to an agent.
	lines := strings.Split(strings.TrimSpace(ve.Error()), "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "-"))
	}
	return fmt.Errorf("invalid args for %s: %s", a.Name, strings.Join(lines, "; "))
}
