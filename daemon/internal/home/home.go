// Package home owns ~/.browser-bridge: config.json, the daemon.pid/daemon.addr files, the daemon
// log and the artifacts folder.
package home

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

const DefaultAddr = "127.0.0.1:9876"

type Config struct {
	Addr         string   `json:"addr"`
	BlockedHosts []string `json:"blockedHosts"`
	ExtensionIDs []string `json:"extensionIds"`
}

// Dir is %USERPROFILE%\.browser-bridge. BRIDGE_HOME overrides it so tests never touch the real one.
func Dir() (string, error) {
	if d := os.Getenv("BRIDGE_HOME"); d != "" {
		return d, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".browser-bridge"), nil
}

func ArtifactsDir(dir string) string { return filepath.Join(dir, "artifacts") }

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// LoadConfig reads dir/config.json. A missing file means defaults. A file that does not parse, has
// an unknown field or an invalid value is an error naming the file: falling back to defaults
// would silently drop the user's blockedHosts.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{Addr: DefaultAddr, BlockedHosts: []string{}, ExtensionIDs: []string{protocol.DefaultExtensionID}}
	path := filepath.Join(dir, "config.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	// Notepad saves UTF-8 with a BOM, which encoding/json rejects.
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := ValidateAddr(cfg.Addr); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	hosts := make([]string, 0, len(cfg.BlockedHosts))
	for _, h := range cfg.BlockedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !hostRe.MatchString(h) {
			return Config{}, fmt.Errorf("%s: blockedHosts entry %q must be a bare host such as bank.com (subdomains are matched automatically)", path, h)
		}
		hosts = append(hosts, h)
	}
	cfg.BlockedHosts = hosts
	if cfg.ExtensionIDs == nil {
		cfg.ExtensionIDs = []string{}
	}
	return cfg, nil
}

// ValidateAddr accepts only loopback addresses: the daemon must never be reachable from another
// machine, whatever config.json or --addr says.
func ValidateAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("addr %q: host must be 127.0.0.1 or localhost", addr)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("addr %q: port must be 1-65535", addr)
	}
	return nil
}

// ResolveAddr applies the precedence --addr > config.json > default.
func ResolveAddr(flagAddr string, cfg Config) (string, error) {
	if flagAddr == "" {
		return cfg.Addr, nil
	}
	return flagAddr, ValidateAddr(flagAddr)
}

func WriteRuntimeFiles(dir string, pid int, addr string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "daemon.addr"), []byte(addr), 0o644)
}

func RemoveRuntimeFiles(dir string) {
	os.Remove(filepath.Join(dir, "daemon.pid"))
	os.Remove(filepath.Join(dir, "daemon.addr"))
}

// OpenLog moves the previous run's log to daemon.log.prev and starts a fresh daemon.log.
func OpenLog(dir string) (*os.File, error) {
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return nil, err
	}
	cur := filepath.Join(logs, "daemon.log")
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, cur+".prev"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(cur, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}
