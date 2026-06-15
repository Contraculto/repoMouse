package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"contraculto.com/repomouse/internal/access"
)

var parseCommandTests = []struct {
	name    string
	cmd     string
	wantOp  access.Op
	wantRepo string
	wantArgv []string
	wantErr  bool
}{
	{
		"git-upload-pack single quotes",
		"git-upload-pack 'myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack double quotes",
		`git-upload-pack "myrepo.git"`,
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack no .git suffix",
		"git-upload-pack 'myrepo'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack leading slash",
		"git-upload-pack '/myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git upload-pack space form",
		"git upload-pack 'myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git", "upload-pack", "__REPO__"}, false,
	},
	{
		"git-receive-pack",
		"git-receive-pack 'myrepo.git'",
		access.OpWrite, "myrepo",
		[]string{"git-receive-pack", "__REPO__"}, false,
	},
	{
		"git receive-pack space form",
		"git receive-pack 'myrepo.git'",
		access.OpWrite, "myrepo",
		[]string{"git", "receive-pack", "__REPO__"}, false,
	},
	{
		"repo with dashes and underscores",
		"git-upload-pack 'my-cool_repo.git'",
		access.OpRead, "my-cool_repo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"unknown command",
		"git-daemon-export-ok",
		0, "", nil, true,
	},
	{
		"empty repo name",
		"git-upload-pack ''",
		0, "", nil, true,
	},
	{
		"path traversal",
		"git-upload-pack '../../../tmp/evil.git'",
		0, "", nil, true,
	},
	{
		"absolute path with traversal",
		"git-upload-pack '/../../tmp/evil.git'",
		0, "", nil, true,
	},
	{
		"nested namespace",
		"git-upload-pack 'team/project.git'",
		access.OpRead, "team/project",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
}

func TestParseCommand(t *testing.T) {
	for _, tt := range parseCommandTests {
		t.Run(tt.name, func(t *testing.T) {
			op, repo, argv, err := parseCommand(tt.cmd)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got op=%v repo=%q", op, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if op != tt.wantOp {
				t.Errorf("op = %v, want %v", op, tt.wantOp)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if len(argv) != len(tt.wantArgv) {
				t.Errorf("argv = %v, want %v", argv, tt.wantArgv)
				return
			}
			for i := range argv {
				if argv[i] != tt.wantArgv[i] {
					t.Errorf("argv[%d] = %q, want %q", i, argv[i], tt.wantArgv[i])
				}
			}
		})
	}
}

// setupTestHome creates an isolated home dir with repos and cache directories.
// It returns the home path, repos directory, and cache directory.
func setupTestHome(t *testing.T) (home, reposDir, cacheDir string) {
	t.Helper()
	home = t.TempDir()
	reposDir = filepath.Join(home, "repos")
	cacheDir = filepath.Join(home, ".repomouse")
	if err := os.MkdirAll(reposDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home, reposDir, cacheDir
}

func writeCache(t *testing.T, cacheDir, reposDir, yaml string) {
	t.Helper()
	yaml = "server:\n  repos_dir: " + reposDir + "\n" + yaml
	if err := os.WriteFile(filepath.Join(cacheDir, "config.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
}

func captureExec(t *testing.T) (gotBin *string, gotArgv *[]string, restore func()) {
	t.Helper()
	var bin string
	var argv []string
	oldExec := execFunc
	execFunc = func(b string, a, e []string) error {
		bin = b
		argv = append([]string(nil), a...)
		return nil
	}
	return &bin, &argv, func() { execFunc = oldExec }
}

func TestRun_allowsAccessAndExecsGit(t *testing.T) {
	_, reposDir, cacheDir := setupTestHome(t)
	writeCache(t, cacheDir, reposDir, `
users:
  alice:
    admin: false
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`)
	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := os.MkdirAll(repoPath, 0750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'myrepo.git'")

	gotBin, gotArgv, restore := captureExec(t)
	defer restore()

	if err := runChecks("alice"); err != nil {
		t.Fatalf("runChecks returned error: %v", err)
	}

	if *gotBin == "" {
		t.Fatal("execFunc was not called")
	}
	wantArgv := []string{"git-receive-pack", repoPath}
	if len(*gotArgv) != len(wantArgv) || (*gotArgv)[0] != wantArgv[0] || (*gotArgv)[1] != wantArgv[1] {
		t.Errorf("exec argv = %v, want %v", *gotArgv, wantArgv)
	}
	if os.Getenv("REPOMOUSE_USER") != "alice" {
		t.Errorf("REPOMOUSE_USER = %q, want alice", os.Getenv("REPOMOUSE_USER"))
	}
	if os.Getenv("REPOMOUSE_REPO") != "myrepo" {
		t.Errorf("REPOMOUSE_REPO = %q, want myrepo", os.Getenv("REPOMOUSE_REPO"))
	}
}

func TestRun_deniesUnknownRepo(t *testing.T) {
	_, reposDir, cacheDir := setupTestHome(t)
	writeCache(t, cacheDir, reposDir, `
users:
  alice:
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`)
	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'otherrepo.git'")

	if err := runChecks("alice"); err == nil {
		t.Fatal("expected error for unknown repo, got nil")
	}
}

func TestRun_deniesUnauthorizedUser(t *testing.T) {
	_, reposDir, cacheDir := setupTestHome(t)
	writeCache(t, cacheDir, reposDir, `
users:
  alice:
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
  bob:
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID bob@laptop"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`)
	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := os.MkdirAll(repoPath, 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'myrepo.git'")

	if err := runChecks("bob"); err == nil {
		t.Fatal("expected error for unauthorized user, got nil")
	}
}

func TestRun_deniesInteractiveShell(t *testing.T) {
	setupTestHome(t)
	t.Setenv("SSH_ORIGINAL_COMMAND", "")

	if err := runChecks("alice"); err == nil {
		t.Fatal("expected error for interactive shell, got nil")
	}
}

func TestRun_regeneratesMissingCache(t *testing.T) {
	// This test exercises loadConfigCache's self-healing path by initializing
	// the admin repo instead of the cache.
	_, reposDir, cacheDir := setupTestHome(t)

	adminPath := filepath.Join(reposDir, "admin.git")
	if err := os.MkdirAll(adminPath, 0750); err != nil {
		t.Fatal(err)
	}

	cfgYAML := []byte("server:\n  repos_dir: " + reposDir + "\nusers:\n  alice:\n    admin: false\n    keys:\n      - \"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop\"\nrepos:\n  myrepo:\n    rules:\n      - \"RW+ = alice\"\n")

	// Create a bare admin repo with config.yaml at HEAD.
	if out, err := execGit(t, "git", "init", "--bare", adminPath); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	tmpWork := t.TempDir()
	if out, err := execGit(t, "git", "-C", tmpWork, "init"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := execGit(t, "git", "-C", tmpWork, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	if out, err := execGit(t, "git", "-C", tmpWork, "config", "user.name", "Test"); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(tmpWork, "config.yaml"), cfgYAML, 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := execGit(t, "git", "-C", tmpWork, "add", "config.yaml"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := execGit(t, "git", "-C", tmpWork, "commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	if out, err := execGit(t, "git", "-C", tmpWork, "push", adminPath, "HEAD:main"); err != nil {
		t.Fatalf("git push: %v\n%s", err, out)
	}
	if out, err := execGit(t, "git", "-C", adminPath, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatalf("git symbolic-ref: %v\n%s", err, out)
	}

	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := os.MkdirAll(repoPath, 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'myrepo.git'")

	// Ensure cache is missing.
	if _, err := os.Stat(filepath.Join(cacheDir, "config.yaml")); err == nil {
		t.Fatal("cache should be missing for this test")
	}

	gotBin, gotArgv, restore := captureExec(t)
	defer restore()

	if err := runChecks("alice"); err != nil {
		t.Fatalf("runChecks returned error: %v", err)
	}

	if *gotBin == "" {
		t.Fatal("execFunc was not called")
	}
	wantArgv := []string{"git-receive-pack", repoPath}
	if len(*gotArgv) != len(wantArgv) || (*gotArgv)[0] != wantArgv[0] || (*gotArgv)[1] != wantArgv[1] {
		t.Errorf("exec argv = %v, want %v", *gotArgv, wantArgv)
	}

	// Cache should now exist.
	if _, err := os.Stat(filepath.Join(cacheDir, "config.yaml")); err != nil {
		t.Errorf("cache was not regenerated: %v", err)
	}
}

func execGit(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	return cmd.CombinedOutput()
}
