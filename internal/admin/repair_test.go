package admin

import (
	"os"
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
