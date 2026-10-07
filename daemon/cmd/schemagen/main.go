// Command schemagen writes schema/protocol.schema.json and the action table of the skill from the
// protocol package. Run it from the daemon folder: go run ./cmd/schemagen
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func main() {
	out := flag.String("out", filepath.Join("..", "schema", "protocol.schema.json"), "output file")
	skill := flag.String("skill", filepath.Join("skill", "browser-bridge", "SKILL.md"), "skill whose action table to rewrite")
	flag.Parse()
	if err := writeSchema(*out); err != nil {
		fail(err)
	}
	fmt.Println("wrote", *out)
	if err := writeToolTable(*skill); err != nil {
		fail(err)
	}
	fmt.Println("wrote", *skill)
}

func writeSchema(out string) error {
	b, err := protocol.Document()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, b, 0o644)
}

func writeToolTable(skill string) error {
	b, err := os.ReadFile(skill)
	if err != nil {
		return err
	}
	b, err = protocol.WithToolTable(b)
	if err != nil {
		return fmt.Errorf("%s: %w", skill, err)
	}
	return os.WriteFile(skill, b, 0o644)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "schemagen:", err)
	os.Exit(1)
}
