package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairHooks_updatesStaleHooks(t *testing.T) {
	home := t.TempDir()
	reposDir := filepath.Join(home, "repos")
	cacheDir := filepath.Join(home, ".repomouse")
	if err := os.MkdirAll(reposDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfgYAML := `
server:
  repos_dir: ` + reposDir + `
users:
  alice:
    admin: true
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`
	if err := os.WriteFile(filepath.Join(cacheDir, "config.yaml"), []byte(cfgYAML), 0600); err != nil {
		t.Fatal(err)
	}

	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := os.MkdirAll(filepath.Join(repoPath, "hooks"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "config"), []byte("[core]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldPath := "/old/path/to/repomouse"
	preReceive := filepath.Join(repoPath, "hooks", "pre-receive")
	postReceive := filepath.Join(repoPath, "hooks", "post-receive")
	if err := os.WriteFile(preReceive, []byte(buildPreReceiveHook(oldPath)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(postReceive, []byte(buildPostReceiveHook(oldPath)), 0755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(home, "repomouse")
	origExec := executablePath
	executablePath = func() (string, error) { return fakeBin, nil }
	defer func() { executablePath = origExec }()

	if err := RepairHooks(); err != nil {
		t.Fatalf("RepairHooks: %v", err)
	}

	preData, err := os.ReadFile(preReceive)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preData), fakeBin) {
		t.Errorf("pre-receive hook not updated to %q: %s", fakeBin, preData)
	}
	postData, err := os.ReadFile(postReceive)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(postData), fakeBin) {
		t.Errorf("post-receive hook not updated to %q: %s", fakeBin, postData)
	}
}

func TestRepairHooks_regeneratesMissingCache(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	reposDir := filepath.Join(home, "repos")
	t.Setenv("HOME", home)

	adminPath := filepath.Join(reposDir, "admin.git")
	if err := os.MkdirAll(reposDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "--bare", adminPath).Run(); err != nil {
		t.Fatal(err)
	}
	cfgYAML := `
server:
  repos_dir: ` + reposDir + `
users:
  alice:
    admin: true
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`
	if err := commitInitialConfig(adminPath, cfgYAML); err != nil {
		t.Fatalf("commitInitialConfig: %v", err)
	}

	// No ~/.repomouse/config.yaml: repair-hooks must rebuild it from the admin
	// repo instead of failing, since it is the recovery tool after a move.
	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := ensureRepos(mustParse(t, cfgYAML), "/old/repomouse"); err != nil {
		t.Fatalf("ensureRepos: %v", err)
	}

	fakeBin := filepath.Join(home, "bin", "repomouse")
	origExec := executablePath
	executablePath = func() (string, error) { return fakeBin, nil }
	defer func() { executablePath = origExec }()

	if err := RepairHooks(); err != nil {
		t.Fatalf("RepairHooks with missing cache: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".repomouse", "config.yaml")); err != nil {
		t.Errorf("cache was not regenerated: %v", err)
	}
	hook, err := os.ReadFile(filepath.Join(repoPath, "hooks", "pre-receive"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hook), fakeBin) {
		t.Errorf("hook not repaired: %s", hook)
	}
}
