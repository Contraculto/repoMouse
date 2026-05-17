package prereceive

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"contraculto.com/repomouse/internal/access"
	"contraculto.com/repomouse/internal/config"
)

const zeroSHA = "0000000000000000000000000000000000000000"

// Run implements the pre-receive hook logic.
// It reads old/new/refname triplets from stdin and denies non-fast-forward
// pushes (and ref deletions) unless the user has RW+ on the repo.
func Run(username string) error {
	repoName := os.Getenv("REPOMOUSE_REPO")
	if repoName == "" {
		fmt.Fprintln(os.Stderr, "repomouse: REPOMOUSE_REPO not set — hook misconfigured")
		os.Exit(1)
	}

	cachePath, err := config.CachePath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadCache(cachePath)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 3 {
			continue
		}
		oldSHA, newSHA, refname := parts[0], parts[1], parts[2]

		if oldSHA == zeroSHA {
			continue // creating a new ref — always allowed for any writer
		}

		isForce := newSHA == zeroSHA || !isFastForward(oldSHA, newSHA)
		if !isForce {
			continue
		}

		if !access.Check(cfg, username, repoName, access.OpForce) {
			fmt.Fprintf(os.Stderr, "repomouse: %s: force push to %s denied for %q\n",
				repoName, refname, username)
			os.Exit(1)
		}
	}
	return scanner.Err()
}

// isFastForward returns true if newSHA is a descendant of oldSHA.
// git merge-base --is-ancestor exits 0 when true, non-zero when false.
func isFastForward(oldSHA, newSHA string) bool {
	return exec.Command("git", "merge-base", "--is-ancestor", oldSHA, newSHA).Run() == nil
}
