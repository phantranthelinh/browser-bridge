package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/client"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

const callUsage = `usage: bridge call <action> --session <name> [key=value ...] [--json <object> | --json-file <file>] [--timeout <ms>]

  key=value     one argument; booleans and numbers follow the action's schema,
                and a repeated key makes a list
  key:=<json>   one argument as raw JSON, e.g. params:='{"depth":1}'
  --json        every argument as one JSON object
  --json-file   read that object from a file, or from stdin with -
  --timeout     milliseconds before the action fails with TIMEOUT (max 120000)

Prints the response envelope. Exit code 0 when ok, 1 when not, 2 when the daemon cannot be reached.
`

func callCmd(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	session := fs.String("session", "", "")
	jsonArgs := fs.String("json", "", "")
	jsonFile := fs.String("json-file", "", "")
	timeout := fs.Int("timeout", 0, "")
	positional, err := parseInterspersed(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, callUsage)
		return 0
	}
	if err != nil || len(positional) == 0 || *session == "" {
		if err != nil {
			fmt.Fprintln(stderr, "bridge call:", err)
		}
		fmt.Fprint(stderr, callUsage)
		return 2
	}
	action, pairs := positional[0], positional[1:]

	var base []byte
	switch {
	case *jsonArgs != "" && *jsonFile != "":
		fmt.Fprintln(stderr, "bridge call: give --json or --json-file, not both")
		return 2
	case *jsonArgs != "":
		base = []byte(*jsonArgs)
	case *jsonFile == "-":
		base, err = io.ReadAll(stdin)
	case *jsonFile != "":
		base, err = os.ReadFile(*jsonFile)
	}
	if err != nil {
		fmt.Fprintln(stderr, "bridge call:", err)
		return 2
	}
	argsJSON, err := buildArgs(action, base, pairs)
	if err != nil {
		fmt.Fprintln(stderr, "bridge call:", err)
		return 2
	}

	e, ok := loadHome(stderr)
	if !ok {
		return 2
	}
	addr, err := e.daemonAddr()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 2
	}
	resp, raw, err := client.New(addr).Command(ctx, protocol.Request{Action: action, Args: argsJSON, Session: *session, TimeoutMs: *timeout})
	if err != nil {
		fmt.Fprintf(stderr, "bridge: %v\n  Start the daemon with: bridge start\n", err)
		return 2
	}
	stdout.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
	if !resp.OK {
		return 1
	}
	return 0
}

// parseInterspersed parses flags wherever they appear among the positional arguments, so that
// "call click --session s selector=@e1" and "call click selector=@e1 --session s" both work.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// buildArgs merges the --json object with key=value pairs, which win. A value is converted to the
// type the action's schema gives its key, so newTab=true is a boolean and amount=300 a number;
// anything the schema does not know stays a string, and the daemon's validation says what is
// wrong with it.
func buildArgs(action string, base []byte, pairs []string) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(base) > 0 {
		if err := json.Unmarshal(base, &obj); err != nil {
			return nil, fmt.Errorf("the --json arguments are not a JSON object: %v", err)
		}
	}
	types := propertyTypes(action)
	lists := map[string][]json.RawMessage{}
	for _, p := range pairs {
		key, value, found := strings.Cut(p, "=")
		if !found || key == "" || key == ":" {
			return nil, fmt.Errorf("%q is not key=value", p)
		}
		if name, isJSON := strings.CutSuffix(key, ":"); isJSON {
			if !json.Valid([]byte(value)) {
				return nil, fmt.Errorf("%s:= takes JSON, got %s", name, value)
			}
			obj[name] = json.RawMessage(value)
			continue
		}
		if types[key] == "array" {
			lists[key] = append(lists[key], typed("string", value))
			continue
		}
		obj[key] = typed(types[key], value)
	}
	for key, items := range lists {
		b, _ := json.Marshal(items)
		obj[key] = b
	}
	if len(obj) == 0 && len(base) == 0 {
		return nil, nil
	}
	return json.Marshal(obj)
}

func typed(schemaType, value string) json.RawMessage {
	switch schemaType {
	case "boolean":
		if b, err := strconv.ParseBool(value); err == nil {
			return json.RawMessage(strconv.FormatBool(b))
		}
	case "integer", "number":
		var n float64
		if json.Unmarshal([]byte(value), &n) == nil {
			return json.RawMessage(value)
		}
	case "object":
		if json.Valid([]byte(value)) {
			return json.RawMessage(value)
		}
	}
	b, _ := json.Marshal(value)
	return b
}

// propertyTypes maps each argument of the action to its JSON Schema type.
func propertyTypes(action string) map[string]string {
	types := map[string]string{}
	a, ok := protocol.Lookup(action)
	if !ok {
		return types
	}
	for name, p := range protocol.SchemaFor(a.Args).Properties.FromOldest() {
		types[name] = p.Type
	}
	return types
}
