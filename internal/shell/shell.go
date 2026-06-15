package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"contraculto.com/repomouse/internal/access"
	"contraculto.com/repomouse/internal/config"
)

// execFunc matches syscall.Exec and is swapable for testing.
var execFunc = syscall.Exec

// Run handles the SSH forced command for the given username.
// It reads SSH_ORIGINAL_COMMAND, checks permissions, then replaces
// the current process with the appropriate git command.
func Run(username string) error {
	if err := runChecks(username); err != nil {
		fmt.Fprintf(os.Stderr, "repomouse: %s: %v\n", username, err)
		os.Exit(1)
	}
	return nil
}

// runChecks performs the shell access check and, on success, execs the real git
// command. It returns an error for deny conditions or setup problems so tests
// can verify behavior without terminating the process.
func runChecks(username string) error {
	original := os.Getenv("SSH_ORIGINAL_COMMAND")
	if original == "" {
		return fmt.Errorf("interactive shell access is not permitted")
	}

	op, repoName, argv, err := parseCommand(original)
	if err != nil {
		return err
	}

	cfg, err := loadConfigCache()
	if err != nil {
		return err
	}

	if !checkAccess(cfg, username, repoName, op) {
		return fmt.Errorf("access denied: %s %q", opName(op), repoName)
	}

	repoPath := config.RepoPath(cfg.ReposDir, repoName)
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("repository %q does not exist", repoName)
	}

	fmt.Fprintf(os.Stderr, "repomouse: access allowed: %s %s %q\n", username, opName(op), repoName)

	// Expose context to hooks (pre-receive checks force-push permission).
	os.Setenv("REPOMOUSE_USER", username)
	os.Setenv("REPOMOUSE_REPO", repoName)

	// Substitute the repo path and exec.
	for i, a := range argv {
		if a == "__REPO__" {
			argv[i] = repoPath
		}
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("cannot find %s: %w", argv[0], err)
	}
	return execFunc(bin, argv, os.Environ())
}

// loadConfigCache reads the cached config. If the cache is missing, it attempts
// to regenerate it from the admin repo so a deleted cache does not lock everyone
// out until an admin logs in server-side.
func loadConfigCache() (*config.Config, error) {
	cachePath, err := config.CachePath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadCache(cachePath)
	if err == nil {
		return cfg, nil
	}

	adminPath, err := config.AdminRepoPath()
	if err != nil {
		return nil, fmt.Errorf("config cache unreadable and admin repo path unavailable: %w", err)
	}
	data, cfg, err := config.Load(adminPath)
	if err != nil {
		return nil, fmt.Errorf("config cache unreadable and admin repo unavailable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "repomouse: regenerated config cache from admin repo\n")
	return cfg, nil
}

func opName(op access.Op) string {
	switch op {
	case access.OpRead:
		return "read"
	case access.OpWrite:
		return "write"
	case access.OpForce:
		return "force"
	}
	return "unknown"
}

// checkAccess handles the admin repo as a special case.
func checkAccess(cfg *config.Config, username, repoName string, op access.Op) bool {
	if repoName == "admin" {
		u, ok := cfg.Users[username]
		return ok && u.Admin
	}
	return access.Check(cfg, username, repoName, op)
}

type gitPattern struct {
	prefix string
	op     access.Op
	argv   []string // __REPO__ is replaced with the actual path
}

var patterns = []gitPattern{
	{"git-upload-pack ", access.OpRead, []string{"git-upload-pack", "__REPO__"}},
	{"git upload-pack ", access.OpRead, []string{"git", "upload-pack", "__REPO__"}},
	{"git-receive-pack ", access.OpWrite, []string{"git-receive-pack", "__REPO__"}},
	{"git receive-pack ", access.OpWrite, []string{"git", "receive-pack", "__REPO__"}},
}

func parseCommand(cmd string) (access.Op, string, []string, error) {
	for _, p := range patterns {
		if strings.HasPrefix(cmd, p.prefix) {
			raw := strings.TrimSpace(cmd[len(p.prefix):])
			raw = strings.Trim(raw, "'\"")
			raw = strings.TrimPrefix(raw, "/")
			name := strings.TrimSuffix(raw, ".git")
			if err := config.ValidateRepoName(name); err != nil {
				return 0, "", nil, fmt.Errorf("invalid repository name in %q: %w", cmd, err)
			}
			argv := make([]string, len(p.argv))
			copy(argv, p.argv)
			return p.op, name, argv, nil
		}
	}
	return 0, "", nil, fmt.Errorf("unsupported git command: %q", cmd)
}
