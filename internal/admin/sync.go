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

	fmt.Fprintln(os.Stderr, "repomouse: sync started")

	data, cfg, err := config.Load(adminPath)
	if err != nil {
		return err
	}

	if err := writeCache(data); err != nil {
		return fmt.Errorf("writing cache: %w", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}
	// Resolve symlinks so the authorized_keys command= points to the real binary.
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	if err := rebuildAuthorizedKeys(cfg, exePath); err != nil {
		return fmt.Errorf("rebuilding authorized_keys: %w", err)
	}

	if err := ensureRepos(cfg, exePath); err != nil {
		return fmt.Errorf("ensuring repos: %w", err)
	}

	fmt.Fprintln(os.Stderr, "repomouse: sync complete")
	return nil
}

func writeCache(data []byte) error {
	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return err
	}
	return os.WriteFile(cachePath, data, 0600)
}

func rebuildAuthorizedKeys(cfg *config.Config, exePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}

	// Sort users for a deterministic, diff-friendly file.
	names := make([]string, 0, len(cfg.Users))
	for u := range cfg.Users {
		names = append(names, u)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("# managed by repomouse — do not edit manually\n")

	for _, username := range names {
		user := cfg.Users[username]
		for _, key := range user.Keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
		fmt.Fprintf(&sb,
			"command=%q,restrict,no-pty %s\n",
			exePath+" shell "+username,
			key,
		)
		}
	}

	akPath := filepath.Join(sshDir, "authorized_keys")
	return os.WriteFile(akPath, []byte(sb.String()), 0600)
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
			fmt.Fprintf(os.Stderr, "repomouse: created repository %q\n", name)
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
	hookPath := filepath.Join(repoPath, "hooks", "pre-receive")
	hook := fmt.Sprintf("#!/bin/sh\nexec %q pre-receive \"$REPOMOUSE_USER\"\n", exePath)
	return os.WriteFile(hookPath, []byte(hook), 0755)
}
