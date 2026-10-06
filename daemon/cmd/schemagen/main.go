// Command schemagen writes schema/protocol.schema.json from the protocol package.
// Run it from the daemon folder: go run ./cmd/schemagen
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
	flag.Parse()
	b, err := protocol.Document()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(*out), 0o755)
	}
	if err == nil {
		err = os.WriteFile(*out, b, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
