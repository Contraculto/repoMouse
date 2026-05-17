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

// Run handles the SSH forced command for the given username.
// It reads SSH_ORIGINAL_COMMAND, checks permissions, then replaces
// the current process with the appropriate git command.
func Run(username string) error {
	original := os.Getenv("SSH_ORIGINAL_COMMAND")
	if original == "" {
		fmt.Fprintln(os.Stderr, "repomouse: interactive shell access is not permitted")
		os.Exit(1)
	}

	op, repoName, argv, err := parseCommand(original)
	if err != nil {
		return err
	}

	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadCache(cachePath)
	if err != nil {
		return err
	}

	if !checkAccess(cfg, username, repoName, op) {
		fmt.Fprintf(os.Stderr, "repomouse: %s: access denied for %q\n", username, repoName)
		os.Exit(1)
	}

	repoPath := config.RepoPath(cfg.ReposDir, repoName)
	if _, err := os.Stat(repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "repomouse: repository %q does not exist\n", repoName)
		os.Exit(1)
	}

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
	return syscall.Exec(bin, argv, os.Environ())
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
			if name == "" {
				return 0, "", nil, fmt.Errorf("empty repository name in: %q", cmd)
			}
			argv := make([]string, len(p.argv))
			copy(argv, p.argv)
			return p.op, name, argv, nil
		}
	}
	return 0, "", nil, fmt.Errorf("unsupported git command: %q", cmd)
}
