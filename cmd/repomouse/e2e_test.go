package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binPath is the CLI, built once in TestMain so these tests exercise the real
// binary end to end: argv dispatch → SSH forced command → git → hooks → sync.
var binPath string

const (
	aliceKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDalice alice@laptop"
	bobKey   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDbob bob@laptop"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git not available: skipping end-to-end tests")
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "repomouse-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating temp dir: %v\n", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "repomouse")

	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building CLI: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func requireE2E(t *testing.T) {
	t.Helper()
	if binPath == "" {
		t.Skip("git not available")
	}
}

// testServer is a sandboxed repoMouse install: its own HOME, repos dir, config
// cache and authorized_keys, driven through the real binary.
type testServer struct {
	t        *testing.T
	home     string
	reposDir string
	wrap     string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	requireE2E(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Isolate git from the developer's real config, keep hook output quiet and
	// allow the ext:: transport the client side of these tests uses.
	gitconfig := `[init]
	defaultBranch = main
[advice]
	defaultBranchName = false
[protocol "ext"]
	allow = always
[user]
	name = E2E
	email = e2e@example.com
`
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0600); err != nil {
		t.Fatal(err)
	}

	// A pre-existing personal key: sync must leave it alone.
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	personal := "# my own login key\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDpersonal rod@laptop\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(personal), 0600); err != nil {
		t.Fatal(err)
	}

	s := &testServer{
		t:        t,
		home:     home,
		reposDir: filepath.Join(home, "repos"),
		wrap:     filepath.Join(home, "ext-wrapper.sh"),
	}

	// Stand-in for sshd: git's ext transport hands it the requested repo as $1,
	// while the SSH forced command receives it in SSH_ORIGINAL_COMMAND.
	script := `#!/bin/sh
export SSH_ORIGINAL_COMMAND="${REPOMOUSE_OP:-git-receive-pack} \"$1\""
exec "$REPOMOUSE_BIN" shell "$REPOMOUSE_USER"
`
	if err := os.WriteFile(s.wrap, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	if out, err := s.cli(nil, "setup", "--admin-user", "alice", "--admin-key", aliceKey); err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	return s
}

// cli runs the built binary with extra environment variables.
func (s *testServer) cli(env []string, args ...string) (string, error) {
	s.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// git runs a git command with extra environment variables.
func (s *testServer) git(env []string, dir string, args ...string) (string, error) {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *testServer) mustGit(env []string, dir string, args ...string) string {
	s.t.Helper()
	out, err := s.git(env, dir, args...)
	if err != nil {
		s.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// pushConfig commits config.yaml to the admin repo. Its post-receive hook runs
// `repomouse sync`, exactly like a real config change.
func (s *testServer) pushConfig(yaml string) {
	s.t.Helper()
	work := filepath.Join(s.home, "admin-work")
	s.mustGit(nil, s.home, "clone", "-q", filepath.Join(s.reposDir, "admin.git"), work)
	if err := os.WriteFile(filepath.Join(work, "config.yaml"), []byte(yaml), 0644); err != nil {
		s.t.Fatal(err)
	}
	s.mustGit(nil, work, "add", "config.yaml")
	s.mustGit(nil, work, "commit", "-q", "-m", "config")
	s.mustGit(nil, work, "push", "-q", "origin", "HEAD:main")

	// The clone is pushed to, so later pushes from the same checkout stay clean.
	s.mustGit(nil, work, "pull", "-q", "--ff-only", "origin", "main")
}

func (s *testServer) remote(repo string) string {
	return "ext::" + s.wrap + " " + repo + ".git"
}

func (s *testServer) clientEnv(user, op string) []string {
	return []string{
		"REPOMOUSE_BIN=" + binPath,
		"REPOMOUSE_USER=" + user,
		"REPOMOUSE_OP=" + op,
	}
}

// clientPush pushes from dir as user through the forced-command path.
func (s *testServer) clientPush(user, repo, dir, refspec string, flags ...string) (string, error) {
	s.t.Helper()
	args := append([]string{"push"}, flags...)
	args = append(args, s.remote(repo), refspec)
	return s.git(s.clientEnv(user, "git-receive-pack"), dir, args...)
}

func (s *testServer) pushOK(user, repo, dir, refspec string, flags ...string) {
	s.t.Helper()
	if out, err := s.clientPush(user, repo, dir, refspec, flags...); err != nil {
		s.t.Fatalf("push as %s to %s failed: %v\n%s", user, repo, err, out)
	}
}

func (s *testServer) pushDenied(user, repo, dir, refspec string, flags ...string) string {
	s.t.Helper()
	out, err := s.clientPush(user, repo, dir, refspec, flags...)
	if err == nil {
		s.t.Fatalf("push as %s to %s should have been denied:\n%s", user, repo, out)
	}
	return out
}

func (s *testServer) workRepo(name string) string {
	s.t.Helper()
	dir := filepath.Join(s.home, name)
	s.mustGit(nil, s.home, "init", "-q", "-b", "main", dir)
	return dir
}

// commit writes a file, commits it and returns the new commit SHA.
func (s *testServer) commit(dir, file, content, msg string) string {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		s.t.Fatal(err)
	}
	s.mustGit(nil, dir, "add", file)
	s.mustGit(nil, dir, "commit", "-q", "-m", msg)
	return strings.TrimSpace(s.mustGit(nil, dir, "rev-parse", "HEAD"))
}

func (s *testServer) remoteSHA(repo, ref string) string {
	s.t.Helper()
	return strings.TrimSpace(s.mustGit(nil, filepath.Join(s.reposDir, repo+".git"), "rev-parse", ref))
}

func (s *testServer) configYAML() string {
	return `
server:
  repos_dir: ` + s.reposDir + `
users:
  alice:
    admin: true
    keys:
      - "` + aliceKey + `"
  bob:
    admin: false
    keys:
      - "` + bobKey + `"
repos:
  proj:
    rules:
      - "RW+ = alice"
      - "RW  = bob"
  docs:
    rules:
      - "R = @all"
`
}

// TestE2E_setupAndConfigSync covers setup plus the admin-push → post-receive →
// sync loop, including authorized_keys merging and hook installation.
func TestE2E_setupAndConfigSync(t *testing.T) {
	s := newTestServer(t)
	s.pushConfig(s.configYAML())

	for _, repo := range []string{"admin", "proj", "docs"} {
		if _, err := os.Stat(filepath.Join(s.reposDir, repo+".git", "HEAD")); err != nil {
			t.Errorf("%s repo not created: %v", repo, err)
		}
	}

	cache, err := os.ReadFile(filepath.Join(s.home, ".repomouse", "config.yaml"))
	if err != nil {
		t.Fatalf("config cache not written: %v", err)
	}
	if !strings.Contains(string(cache), "proj") {
		t.Errorf("cache does not hold the pushed config:\n%s", cache)
	}

	keys, err := os.ReadFile(filepath.Join(s.home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(keys), "rod@laptop") || !strings.Contains(string(keys), "# my own login key") {
		t.Errorf("personal key was dropped:\n%s", keys)
	}
	want := `command="` + binPath + ` shell alice",restrict,no-pty ` + aliceKey + ` repoMouse:alice`
	if !strings.Contains(string(keys), want) {
		t.Errorf("missing managed line %q in:\n%s", want, keys)
	}

	hook, err := os.ReadFile(filepath.Join(s.reposDir, "proj.git", "hooks", "pre-receive"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hook), binPath) || !strings.Contains(string(hook), "pre-receive") {
		t.Errorf("pre-receive hook not installed correctly:\n%s", hook)
	}
}

// TestE2E_pushPermissions drives a real git client through the forced command:
// access check, exec of git-receive-pack, pre-receive hook force-push rules.
func TestE2E_pushPermissions(t *testing.T) {
	s := newTestServer(t)
	s.pushConfig(s.configYAML())

	work := s.workRepo("work")

	// alice has RW+: plain push.
	first := s.commit(work, "a.txt", "a\n", "A")
	s.pushOK("alice", "proj", work, "HEAD:refs/heads/main")
	if got := s.remoteSHA("proj", "refs/heads/main"); got != first {
		t.Fatalf("remote main = %s, want %s", got, first)
	}

	// bob has RW: fast-forward push is fine.
	second := s.commit(work, "b.txt", "b\n", "B")
	s.pushOK("bob", "proj", work, "HEAD:refs/heads/main")
	if got := s.remoteSHA("proj", "refs/heads/main"); got != second {
		t.Fatalf("remote main = %s, want %s", got, second)
	}

	// bob rewinding history is a force push: denied, remote unchanged.
	s.mustGit(nil, work, "reset", "-q", "--hard", first)
	out := s.pushDenied("bob", "proj", work, "HEAD:refs/heads/main", "--force")
	if !strings.Contains(out, "force push") || !strings.Contains(out, "denied") {
		t.Errorf("unexpected denial message:\n%s", out)
	}
	if got := s.remoteSHA("proj", "refs/heads/main"); got != second {
		t.Errorf("remote main moved to %s despite denial, want %s", got, second)
	}

	// alice has RW+: the same diverged history is accepted.
	third := s.commit(work, "c.txt", "c\n", "C")
	s.pushOK("alice", "proj", work, "HEAD:refs/heads/main", "--force")
	if got := s.remoteSHA("proj", "refs/heads/main"); got != third {
		t.Errorf("remote main = %s, want %s", got, third)
	}
}

// TestE2E_readAndDeny covers the upload-pack path and the denial paths.
func TestE2E_readAndDeny(t *testing.T) {
	s := newTestServer(t)
	s.pushConfig(s.configYAML())

	work := s.workRepo("work")
	head := s.commit(work, "a.txt", "a\n", "A")
	s.pushOK("alice", "proj", work, "HEAD:refs/heads/main")

	// Read access works through git-upload-pack.
	dest := filepath.Join(s.home, "clone")
	if out, err := s.git(s.clientEnv("bob", "git-upload-pack"), s.home,
		"clone", "-q", s.remote("proj"), dest); err != nil {
		t.Fatalf("clone failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(s.mustGit(nil, dest, "rev-parse", "HEAD")); got != head {
		t.Errorf("clone HEAD = %s, want %s", got, head)
	}

	// bob only has R on docs: pushing there is denied.
	out := s.pushDenied("bob", "docs", work, "HEAD:refs/heads/main")
	if !strings.Contains(out, "access denied") {
		t.Errorf("unexpected denial message:\n%s", out)
	}

	// Repos not in the config are denied outright.
	out = s.pushDenied("alice", "nope", work, "HEAD:refs/heads/main")
	if !strings.Contains(out, "access denied") {
		t.Errorf("unexpected denial message:\n%s", out)
	}

	// The admin repo is not in the config: admins get in, others do not.
	if _, err := os.Stat(filepath.Join(s.reposDir, "nope.git")); err == nil {
		t.Error("repo outside the config was created")
	}
}

// TestE2E_cli covers the parts of cmd/repomouse that are not package logic:
// subcommand dispatch, usage output and exit codes.
func TestE2E_cli(t *testing.T) {
	requireE2E(t)
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"help", []string{"--help"}, 0, "pre-receive"},
		{"no args", nil, 1, "Commands:"},
		{"unknown command", []string{"bogus"}, 1, `unknown command "bogus"`},
		{"shell without user", []string{"shell"}, 1, "missing username"},
		{"pre-receive without user", []string{"pre-receive"}, 1, "missing username"},
		{"setup without flags", []string{"setup"}, 1, "--admin-user and --admin-key are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command(binPath, tt.args...).CombinedOutput()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("running CLI: %v", err)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tt.wantOut, out)
			}
		})
	}
}
