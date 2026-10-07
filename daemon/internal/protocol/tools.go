package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// FlatInputSchema is InputSchema without oneOf, dependentRequired and const. Several tool-calling
// APIs refuse them in a tool's schema, and small models misread them. The daemon still validates
// against the full schema, and the descriptions say which arguments go together.
func FlatInputSchema(a Action) json.RawMessage {
	s := SchemaFor(a.Args)
	s.OneOf = nil
	s.DependentRequired = nil
	for _, p := range s.Properties.FromOldest() {
		p.Const = nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

// argSummary lists an action's arguments the way the tool table shows them: `url` is required,
// `newTab?` optional, and `value`/`label` means exactly one of them.
func argSummary(a Action) string {
	s := SchemaFor(a.Args)
	var choice []string
	for _, alt := range s.OneOf {
		choice = append(choice, alt.Required...)
	}
	var parts []string
	for name := range s.Properties.FromOldest() {
		switch {
		case slices.Contains(choice, name):
			if name == choice[0] {
				parts = append(parts, "`"+strings.Join(choice, "`/`")+"`")
			}
		case slices.Contains(s.Required, name):
			parts = append(parts, "`"+name+"`")
		default:
			parts = append(parts, "`"+name+"?`")
		}
	}
	return strings.Join(parts, ", ")
}

// ToolTable is the action table of SKILL.md. It is generated, so the skill always names the
// arguments the daemon accepts.
func ToolTable() string {
	var b strings.Builder
	b.WriteString("| Action | Arguments | What it does |\n|---|---|---|\n")
	for _, a := range Actions {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", a.Name, argSummary(a), strings.ReplaceAll(a.Description, "|", `\|`))
	}
	return b.String()
}

const (
	toolsBegin = "<!-- tools:begin -->\n"
	toolsEnd   = "<!-- tools:end -->"
)

// WithToolTable returns doc with the text between its tools:begin and tools:end markers replaced
// by ToolTable.
func WithToolTable(doc []byte) ([]byte, error) {
	start := bytes.Index(doc, []byte(toolsBegin))
	end := bytes.Index(doc, []byte(toolsEnd))
	if start < 0 || end < start {
		return nil, errors.New("no " + strings.TrimSpace(toolsBegin) + " ... " + toolsEnd + " markers")
	}
	start += len(toolsBegin)
	out := slices.Concat(doc[:start], []byte(ToolTable()), doc[end:])
	return out, nil
}
