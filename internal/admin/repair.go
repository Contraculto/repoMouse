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

// RepairHooks rewrites pre-receive and post-receive hooks in every repo under
// repos_dir so they point to the current repomouse binary path. Use this after
// moving the binary.
func RepairHooks() error {
	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadCache(cachePath)
	if err != nil {
		return fmt.Errorf("loading config cache: %w", err)
	}

	exePath, err := executablePath()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
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
			fmt.Fprintf(os.Stderr, "repomouse: repaired %s\n", preReceive)
		}

		if updated, err := repairHook(postReceive, buildPostReceiveHook(exePath)); err != nil {
			return fmt.Errorf("repairing %s: %w", postReceive, err)
		} else if updated {
			repaired++
			fmt.Fprintf(os.Stderr, "repomouse: repaired %s\n", postReceive)
		}
	}

	fmt.Fprintf(os.Stderr, "repomouse: repaired %d hook(s)\n", repaired)
	return nil
}

func isBareRepo(path string) bool {
	// A bare git repo has a HEAD file and a config file.
	_, errHead := os.Stat(filepath.Join(path, "HEAD"))
	_, errConfig := os.Stat(filepath.Join(path, "config"))
	return errHead == nil && errConfig == nil
}

func buildPreReceiveHook(exePath string) string {
	return fmt.Sprintf("#!/bin/sh\nexec %q pre-receive \"$REPOMOUSE_USER\"\n", exePath)
}

func buildPostReceiveHook(exePath string) string {
	return fmt.Sprintf("#!/bin/sh\nexec %q sync\n", exePath)
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
	return true, os.WriteFile(path, []byte(content), 0755)
}

func isRepomouseHook(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return false
	}
	return strings.TrimSpace(lines[0]) == "#!/bin/sh" &&
		strings.Contains(lines[1], "repomouse") &&
		strings.HasPrefix(strings.TrimSpace(lines[1]), "exec ")
}
