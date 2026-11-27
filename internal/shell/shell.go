package shell

import (
	"fmt"
	"os"
	"os/exec"
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
		fmt.Fprintf(os.Stderr, "repoMouse: %s: %v\n", username, err)
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

	if !access.CheckRepo(cfg, username, repoName, op) {
		return fmt.Errorf("access denied: %s %q", opName(op), repoName)
	}

	repoPath := config.RepoPath(cfg.ReposDir, repoName)
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("repository %q does not exist", repoName)
	}

	fmt.Fprintf(os.Stderr, "repoMouse: access allowed: %s %s %q\n", username, opName(op), repoName)

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

// loadConfigCache reads the config through the shared cache-with-fallback
// loader, so a deleted cache is rebuilt from the admin repo.
func loadConfigCache() (*config.Config, error) {
	return config.LoadCached()
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
