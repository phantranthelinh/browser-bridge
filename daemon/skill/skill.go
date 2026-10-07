// Package skill holds the agent skill that bridge install-skill copies into Claude Code and Codex.
// It is built into the binary, so an installed skill always describes the bridge that runs it.
package skill

import "embed"

// Name is the skill's folder name in an agent's skills folder.
const Name = "browser-bridge"

//go:embed browser-bridge
var FS embed.FS
