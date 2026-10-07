package main

import (
	"cmp"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/phantranthelinh/browser-bridge/daemon/skill"
)

type agentHome struct {
	name string
	dir  string
}

// agentHomes are the agents install-skill knows, each with the folder that holds its skills/.
// CLAUDE_CONFIG_DIR and CODEX_HOME move those folders, for the agents and for us.
func agentHomes() ([]agentHome, error) {
	user, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []agentHome{
		{"Claude Code", cmp.Or(os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(user, ".claude"))},
		{"Codex", cmp.Or(os.Getenv("CODEX_HOME"), filepath.Join(user, ".codex"))},
	}, nil
}

func installSkillCmd(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	remove := flags.Bool("remove", false, "remove the skill instead")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	homes, err := agentHomes()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	code := 0
	for _, h := range homes {
		dest := filepath.Join(h.dir, "skills", skill.Name)
		if *remove {
			if _, err := os.Stat(dest); err != nil {
				continue
			}
			if err := os.RemoveAll(dest); err != nil {
				fmt.Fprintf(stderr, "bridge: %s: %v\n", h.name, err)
				code = 1
				continue
			}
			fmt.Fprintf(stdout, "%s: removed %s\n", h.name, dest)
			continue
		}
		// Only for agents that are installed: creating ~/.codex would not make Codex appear.
		if _, err := os.Stat(h.dir); err != nil {
			fmt.Fprintf(stdout, "%s: not installed (no %s), skipped\n", h.name, h.dir)
			continue
		}
		if err := writeSkill(dest); err != nil {
			fmt.Fprintf(stderr, "bridge: %s: %v\n", h.name, err)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "%s: installed %s\n", h.name, dest)
	}
	if !*remove {
		printMCPSetup(stdout)
	}
	return code
}

// writeSkill replaces dest with the skill built into this binary, so files an older version had
// and this one dropped do not linger.
func writeSkill(dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	root, err := fs.Sub(skill.FS, skill.Name)
	if err != nil {
		return err
	}
	return os.CopyFS(dest, root)
}

func printMCPSetup(stdout io.Writer) {
	exe, err := os.Executable()
	if err != nil {
		exe = "bridge"
	}
	fmt.Fprintf(stdout, `
Claude Code reads the skill. Agents that speak MCP run "bridge mcp" instead:
  Codex: add to config.toml
    [mcp_servers.browser-bridge]
    command = '%[1]s'
    args = ["mcp"]
  Claude Code, to use MCP rather than the skill:
    claude mcp add browser-bridge -- "%[1]s" mcp
  Any other MCP client (a local model's harness): command "%[1]s", args ["mcp"], over stdio
`, exe)
}
