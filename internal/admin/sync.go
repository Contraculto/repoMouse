package admin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"contraculto.com/repomouse/internal/config"
)

// Sync reads the config from the admin repo, writes the cache, rebuilds
// authorized_keys, and creates any repos defined in config that don't exist yet.
func Sync() error {
	adminPath, err := config.AdminRepoPath()
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "repoMouse: sync started")

	data, cfg, err := config.Load(adminPath)
	if err != nil {
		return err
	}

	if err := writeCache(data); err != nil {
		return fmt.Errorf("writing cache: %w", err)
	}

	exePath, err := binaryPath()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	if err := rebuildAuthorizedKeys(cfg, exePath); err != nil {
		return fmt.Errorf("rebuilding authorized_keys: %w", err)
	}

	if err := ensureRepos(cfg, exePath); err != nil {
		return fmt.Errorf("ensuring repos: %w", err)
	}

	fmt.Fprintln(os.Stderr, "repoMouse: sync complete")
	return nil
}

func writeCache(data []byte) error {
	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	return config.WriteCache(cachePath, data)
}

// authorizedKeysHeader labels the generated file. Only lines repoMouse wrote are
// ever removed — personal login keys and keys owned by other tools are kept.
const authorizedKeysHeader = "# managed by repoMouse — do not edit manually"

// managedTag is appended to the key comment of every generated line, so a stale
// line stays recognizable even if the binary is later renamed or moved.
const managedTag = "repoMouse:"

func rebuildAuthorizedKeys(cfg *config.Config, exePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}
	akPath := filepath.Join(sshDir, "authorized_keys")

	// Keys we did not write must survive the rewrite.
	kept, err := unmanagedLines(akPath, exePath)
	if err != nil {
		return err
	}

	// Sort users for a deterministic, diff-friendly file.
	names := make([]string, 0, len(cfg.Users))
	for u := range cfg.Users {
		names = append(names, u)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(authorizedKeysHeader + "\n")

	for _, username := range names {
		user := cfg.Users[username]
		for _, key := range user.Keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			fmt.Fprintf(&sb,
				"command=%s,restrict,no-pty %s %s%s\n",
				commandQuote(exePath+" shell "+username),
				key,
				managedTag,
				username,
			)
		}
	}

	if len(kept) > 0 {
		sb.WriteString("\n")
		for _, line := range kept {
			sb.WriteString(line + "\n")
		}
	}

	// Rename into place: a crash must not leave a truncated authorized_keys,
	// which would lock every user out.
	return writeFileAtomic(akPath, []byte(sb.String()), 0600)
}

// unmanagedLines returns the existing authorized_keys entries that repoMouse did
// not write, with our own header and managed lines dropped.
func unmanagedLines(path, exePath string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# managed by repoMouse") {
			continue
		}
		if isManagedLine(line, exePath) {
			continue
		}
		kept = append(kept, line)
	}
	return kept, nil
}

// isManagedLine reports whether an authorized_keys line was written by repoMouse.
// Tagged lines are ours whatever the path looks like; lines written before the
// tag existed are recognized by their forced command pointing at the binary.
func isManagedLine(line, exePath string) bool {
	const prefix = `command="`
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	if hasManagedTag(line) {
		return true
	}
	cmd, ok := forcedCommand(line)
	if !ok {
		return false
	}
	fields := strings.Fields(cmd)
	if len(fields) != 3 || fields[1] != "shell" || config.ValidateName(fields[2]) != nil {
		return false
	}
	return fields[0] == exePath || strings.EqualFold(filepath.Base(fields[0]), "repomouse")
}

// forcedCommand extracts the value of command="..." from an authorized_keys line,
// undoing the \\" and \\\\ escapes written by commandQuote.
func forcedCommand(line string) (string, bool) {
	const prefix = `command="`
	rest := strings.TrimPrefix(line, prefix)
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			i++
			if i >= len(rest) {
				return "", false
			}
			b.WriteByte(rest[i])
		case '"':
			return b.String(), true
		default:
			b.WriteByte(rest[i])
		}
	}
	return "", false
}

// hasManagedTag reports whether line ends with a "repoMouse:<username>" tag.
func hasManagedTag(line string) bool {
	i := strings.LastIndex(line, " "+managedTag)
	if i < 0 {
		return false
	}
	return config.ValidateName(line[i+1+len(managedTag):]) == nil
}

// commandQuote quotes a forced command for the command="..." field of
// authorized_keys. OpenSSH's parser only understands \" and \\ inside the
// quotes, so Go's %q (which also emits \n, \t, \x.. escapes) must not be used:
// a path with an odd character would silently become a different command.
func commandQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// writeFileAtomic writes data to path via a temp file in the same directory and
// a rename, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".repomouse-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func ensureRepos(cfg *config.Config, exePath string) error {
	if err := os.MkdirAll(cfg.ReposDir, 0750); err != nil {
		return err
	}
	for name := range cfg.Repos {
		if err := config.ValidateRepoName(name); err != nil {
			return fmt.Errorf("invalid repo name in config: %w", err)
		}
		repoPath := config.RepoPath(cfg.ReposDir, name)
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			if err := initRepo(repoPath, exePath); err != nil {
				return fmt.Errorf("creating repo %q: %w", name, err)
			}
			fmt.Fprintf(os.Stderr, "repoMouse: created repository %q\n", name)
		}
	}
	return nil
}

func initRepo(repoPath, exePath string) error {
	cmd := exec.Command("git", "init", "--bare", repoPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Set default branch to main regardless of git's global default.
	if err := exec.Command("git", "-C", repoPath, "symbolic-ref", "HEAD", "refs/heads/main").Run(); err != nil {
		return err
	}
	// hookPath is created by git init; the hook must be installed atomically so a
	// crash cannot leave a truncated hook that blocks every push.
	hookPath := filepath.Join(repoPath, "hooks", "pre-receive")
	return writeFileAtomic(hookPath, []byte(buildPreReceiveHook(exePath)), 0755)
}
