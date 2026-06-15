package prereceive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo creates a temp bare repo named "myrepo.git" with two branches:
//   - main at commit A
//   - topic at commit B, where B is a fast-forward from A
// It returns the repo path, the SHA of A, and the SHA of B.
func makeRepo(t *testing.T) (repoPath, mainSHA, topicSHA string) {
	t.Helper()
	repoPath = filepath.Join(t.TempDir(), "myrepo.git")
	if err := os.MkdirAll(repoPath, 0750); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("git", "init", "--bare", repoPath)
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	// Create a working tree to make commits.
	work := t.TempDir()
	wrun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	wrun("git", "init", work)
	wrun("git", "remote", "add", "origin", repoPath)
	wrun("git", "config", "user.email", "test@example.com")
	wrun("git", "config", "user.name", "Test")

	// Commit A on main.
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wrun("git", "add", "a.txt")
	wrun("git", "commit", "-m", "A")
	wrun("git", "push", "origin", "HEAD:main")
	mainSHA = wrun("git", "rev-parse", "HEAD")

	// Commit B on topic (fast-forward from A).
	wrun("git", "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wrun("git", "add", "b.txt")
	wrun("git", "commit", "-m", "B")
	wrun("git", "push", "origin", "HEAD:topic")
	topicSHA = wrun("git", "rev-parse", "HEAD")

	return repoPath, mainSHA, topicSHA
}

func writeCache(t *testing.T, home, reposDir string) {
	t.Helper()
	cacheDir := filepath.Join(home, ".repomouse")
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatal(err)
	}
	cfgYAML := `
server:
  repos_dir: ` + reposDir + `
users:
  alice:
    admin: false
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
  bob:
    admin: false
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID bob@laptop"
groups:
  writers:
    - bob
repos:
  myrepo:
    rules:
      - "RW+ = alice"
      - "RW = @writers"
`
	if err := os.WriteFile(filepath.Join(cacheDir, "config.yaml"), []byte(cfgYAML), 0600); err != nil {
		t.Fatal(err)
	}
}

func feed(oldSHA, newSHA, refname string) *bytes.Reader {
	line := oldSHA + " " + newSHA + " " + refname + "\n"
	return bytes.NewReader([]byte(line))
}

func TestRunChecks_fastForwardAllowed(t *testing.T) {
	repoPath, mainSHA, topicSHA := makeRepo(t)
	home := t.TempDir()
	writeCache(t, home, filepath.Dir(repoPath))
	t.Setenv("HOME", home)
	t.Setenv("REPOMOUSE_REPO", "myrepo")

	// Bob has RW; updating main from A to B is a fast-forward.
	if err := runChecks("bob", feed(mainSHA, topicSHA, "refs/heads/main")); err != nil {
		t.Fatalf("fast-forward push denied: %v", err)
	}
}

func TestRunChecks_forcePushDeniedForRW(t *testing.T) {
	repoPath, mainSHA, topicSHA := makeRepo(t)
	home := t.TempDir()
	writeCache(t, home, filepath.Dir(repoPath))
	t.Setenv("HOME", home)
	t.Setenv("REPOMOUSE_REPO", "myrepo")

	// Bob has RW; resetting main from B to A is a force push.
	if err := runChecks("bob", feed(topicSHA, mainSHA, "refs/heads/main")); err == nil {
		t.Fatal("force push should be denied for RW user")
	}
}

func TestRunChecks_forcePushAllowedForRWPlus(t *testing.T) {
	repoPath, mainSHA, topicSHA := makeRepo(t)
	home := t.TempDir()
	writeCache(t, home, filepath.Dir(repoPath))
	t.Setenv("HOME", home)
	t.Setenv("REPOMOUSE_REPO", "myrepo")

	// Alice has RW+; resetting main from B to A is allowed.
	if err := runChecks("alice", feed(topicSHA, mainSHA, "refs/heads/main")); err != nil {
		t.Fatalf("force push should be allowed for RW+ user: %v", err)
	}
}

func TestRunChecks_refDeleteDeniedForRW(t *testing.T) {
	_, mainSHA, _ := makeRepo(t)
	home := t.TempDir()
	// We don't actually need the repo on disk for the access check path,
	// but the cache must reference the same repos_dir if we did.
	writeCache(t, home, "/tmp/does-not-matter")
	t.Setenv("HOME", home)
	t.Setenv("REPOMOUSE_REPO", "myrepo")

	if err := runChecks("bob", feed(mainSHA, zeroSHA, "refs/heads/topic")); err == nil {
		t.Fatal("ref deletion should be denied for RW user")
	}
}

func TestRunChecks_newRefAllowed(t *testing.T) {
	_, _, topicSHA := makeRepo(t)
	home := t.TempDir()
	writeCache(t, home, "/tmp/does-not-matter")
	t.Setenv("HOME", home)
	t.Setenv("REPOMOUSE_REPO", "myrepo")

	// Creating a new ref with oldSHA=zero is always allowed for writers.
	if err := runChecks("bob", feed(zeroSHA, topicSHA, "refs/heads/new")); err != nil {
		t.Fatalf("new ref should be allowed: %v", err)
	}
}
