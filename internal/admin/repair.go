package admin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"contraculto.com/repomouse/internal/config"
)

// executablePath is swappable for testing.
var executablePath = os.Executable

// binaryPath returns the absolute, symlink-resolved path of the running binary.
// Hooks and authorized_keys embed this path, so a symlinked install must be
// resolved to the real file.
func binaryPath() (string, error) {
	p, err := executablePath()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

// RepairHooks rewrites pre-receive and post-receive hooks in every repo under
// repos_dir so they point to the current repomouse binary path. Use this after
// moving the binary.
func RepairHooks() error {
	cfg, err := config.LoadCached()
	if err != nil {
		return err
	}

	exePath, err := binaryPath()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	entries, err := os.ReadDir(cfg.ReposDir)
	if err != nil {
		return fmt.Errorf("reading repos dir: %w", err)
	}

	repaired := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoPath := filepath.Join(cfg.ReposDir, entry.Name())
		if !isBareRepo(repoPath) {
			continue
		}
		preReceive := filepath.Join(repoPath, "hooks", "pre-receive")
		postReceive := filepath.Join(repoPath, "hooks", "post-receive")

		if updated, err := repairHook(preReceive, buildPreReceiveHook(exePath)); err != nil {
			return fmt.Errorf("repairing %s: %w", preReceive, err)
		} else if updated {
			repaired++
			fmt.Fprintf(os.Stderr, "repoMouse: repaired %s\n", preReceive)
		}

		if updated, err := repairHook(postReceive, buildPostReceiveHook(exePath)); err != nil {
			return fmt.Errorf("repairing %s: %w", postReceive, err)
		} else if updated {
			repaired++
			fmt.Fprintf(os.Stderr, "repoMouse: repaired %s\n", postReceive)
		}
	}

	fmt.Fprintf(os.Stderr, "repoMouse: repaired %d hook(s)\n", repaired)
	return nil
}

func isBareRepo(path string) bool {
	// A bare git repo has a HEAD file and a config file.
	_, errHead := os.Stat(filepath.Join(path, "HEAD"))
	_, errConfig := os.Stat(filepath.Join(path, "config"))
	return errHead == nil && errConfig == nil
}

func buildPreReceiveHook(exePath string) string {
	return fmt.Sprintf("#!/bin/sh\nexec %s pre-receive \"$REPOMOUSE_USER\"\n", shQuote(exePath))
}

func buildPostReceiveHook(exePath string) string {
	return fmt.Sprintf("#!/bin/sh\nexec %s sync\n", shQuote(exePath))
}

// shQuote quotes s as a single word for /bin/sh. The hooks are shell scripts, so
// Go quoting (%q) is wrong: a path containing $, a backtick or a backslash would
// be expanded by the shell instead of passed through.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// repairHook overwrites path with content only if path exists and was created
// by repomouse (identified by a shebang and a repomouse exec line).
func repairHook(path, content string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !isRepomouseHook(string(data)) {
		return false, nil
	}
	if string(data) == content {
		return false, nil
	}
	return true, writeFileAtomic(path, []byte(content), 0755)
}

func isRepomouseHook(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return false
	}
	return strings.TrimSpace(lines[0]) == "#!/bin/sh" &&
		strings.Contains(strings.ToLower(lines[1]), "repomouse") &&
		strings.HasPrefix(strings.TrimSpace(lines[1]), "exec ")
}
