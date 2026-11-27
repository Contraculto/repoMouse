package prereceive

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"contraculto.com/repomouse/internal/access"
	"contraculto.com/repomouse/internal/config"
)

const zeroSHA = "0000000000000000000000000000000000000000"

// isZeroSHA reports whether s is the all-zero object name git uses for "no
// object" (40 zeros for SHA-1 repos, 64 for SHA-256 repos).
func isZeroSHA(s string) bool {
	return s != "" && strings.Trim(s, "0") == ""
}

// isObjectName reports whether s is a full SHA-1 or SHA-256 object name. The
// refs come from git, but they are passed to `git merge-base` as arguments, so
// anything unexpected is rejected rather than forwarded.
func isObjectName(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// Run implements the pre-receive hook logic.
// It reads old/new/refname triplets from stdin and denies non-fast-forward
// pushes (and ref deletions) unless the user has RW+ on the repo.
func Run(username string) error {
	if err := runChecks(username, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "repoMouse: %v\n", err)
		os.Exit(1)
	}
	return nil
}

// runChecks performs the actual pre-receive validation and returns an error
// when the push should be rejected. It is separated from Run so tests can
// call it without terminating the process.
func runChecks(username string, stdin io.Reader) error {
	repoName := os.Getenv("REPOMOUSE_REPO")
	if repoName == "" {
		return fmt.Errorf("REPOMOUSE_REPO not set — hook misconfigured")
	}
	if err := config.ValidateRepoName(repoName); err != nil {
		return fmt.Errorf("invalid REPOMOUSE_REPO: %w", err)
	}

	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadCache(cachePath)
	if err != nil {
		return err
	}

	repoPath := config.RepoPath(cfg.ReposDir, repoName)

	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 3 {
			continue
		}
		oldSHA, newSHA, refname := parts[0], parts[1], parts[2]
		if !isObjectName(oldSHA) || !isObjectName(newSHA) {
			return fmt.Errorf("malformed ref update for %s", refname)
		}

		if isZeroSHA(oldSHA) {
			continue // creating a new ref — always allowed for any writer
		}

		isForce := isZeroSHA(newSHA) || !isFastForward(repoPath, oldSHA, newSHA)
		if !isForce {
			continue
		}

		if !access.CheckRepo(cfg, username, repoName, access.OpForce) {
			return fmt.Errorf("%s: force push to %s denied for %q", repoName, refname, username)
		}
	}
	return scanner.Err()
}

// isFastForward returns true if newSHA is a descendant of oldSHA.
// git merge-base --is-ancestor exits 0 when true, non-zero when false.
func isFastForward(repoPath, oldSHA, newSHA string) bool {
	return exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor", oldSHA, newSHA).Run() == nil
}
