package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/phantranthelinh/browser-bridge/daemon/skill"
)

type agentHome struct {
	id   string // what --agent takes
	name string
	dir  string // the folder that holds the agent's skills/
}

func (h agentHome) skillDir() string { return filepath.Join(h.dir, "skills", skill.Name) }

func (h agentHome) installed() bool {
	_, err := os.Stat(h.dir)
	return err == nil
}

func (h agentHome) hasSkill() bool {
	_, err := os.Stat(h.skillDir())
	return err == nil
}

// agentHomes are the agents install-skill knows. CLAUDE_CONFIG_DIR and CODEX_HOME move those
// folders, for the agents and for us; Antigravity has no such variable.
func agentHomes() ([]agentHome, error) {
	user, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []agentHome{
		{"claude", "Claude Code", cmp.Or(os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(user, ".claude"))},
		{"codex", "Codex", cmp.Or(os.Getenv("CODEX_HOME"), filepath.Join(user, ".codex"))},
		{"antigravity", "Antigravity", filepath.Join(user, ".gemini", "config")},
	}, nil
}

func installSkillCmd(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	remove := flags.Bool("remove", false, "remove the skill instead")
	list := flags.Bool("list", false, "print each agent's id, name, state (skill, installed or missing) and folder, tab-separated")
	agents := flags.String("agent", "", "only these agents, comma-separated: claude, codex, antigravity (default: every installed agent)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	homes, err := agentHomes()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	if *list {
		for _, h := range homes {
			state := "missing"
			if h.hasSkill() {
				state = "skill"
			} else if h.installed() {
				state = "installed"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", h.id, h.name, state, h.dir)
		}
		return 0
	}
	chosen := *agents != ""
	if chosen {
		if homes, err = pickAgents(homes, *agents); err != nil {
			fmt.Fprintln(stderr, "bridge:", err)
			return 2
		}
	}
	code := 0
	for _, h := range homes {
		dest := h.skillDir()
		if *remove {
			if !h.hasSkill() {
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
		if !h.installed() {
			if chosen {
				fmt.Fprintf(stderr, "bridge: %s: not installed (no %s)\n", h.name, h.dir)
				code = 1
			} else {
				fmt.Fprintf(stdout, "%s: not installed (no %s), skipped\n", h.name, h.dir)
			}
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

// pickAgents keeps the homes named in a comma-separated list of ids, in the list's order.
func pickAgents(homes []agentHome, list string) ([]agentHome, error) {
	var picked []agentHome
	for id := range strings.SplitSeq(list, ",") {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		i := slices.IndexFunc(homes, func(h agentHome) bool { return h.id == id })
		if i < 0 {
			return nil, fmt.Errorf("unknown agent %q (known: claude, codex, antigravity)", id)
		}
		if !slices.ContainsFunc(picked, func(h agentHome) bool { return h.id == id }) {
			picked = append(picked, homes[i])
		}
	}
	return picked, nil
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
	// JSON needs the backslashes of a Windows path escaped; TOML's '...' strings and the shell do not.
	jsonExe, _ := json.Marshal(exe)
	fmt.Fprintf(stdout, `
Claude Code, Codex and Antigravity read the skill. Agents that speak MCP run "bridge mcp" instead:
  Codex: add to config.toml
    [mcp_servers.browser-bridge]
    command = '%[1]s'
    args = ["mcp"]
  Antigravity: add to mcpServers in ~/.gemini/config/mcp_config.json
    "browser-bridge": { "command": %[2]s, "args": ["mcp"] }
  Claude Code, to use MCP rather than the skill:
    claude mcp add browser-bridge -- "%[1]s" mcp
  Any other MCP client (a local model's harness): command "%[1]s", args ["mcp"], over stdio
`, exe, jsonExe)
}
