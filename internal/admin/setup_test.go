package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetup_initializesServer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeBin := filepath.Join(home, "bin", "repomouse")
	origExec := executablePath
	executablePath = func() (string, error) { return fakeBin, nil }
	defer func() { executablePath = origExec }()

	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
	if err := Setup([]string{"--admin-user", "alice", "--admin-key", key}); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	adminPath := filepath.Join(home, "repos", "admin.git")
	if _, err := os.Stat(filepath.Join(adminPath, "HEAD")); err != nil {
		t.Errorf("admin repo not initialized: %v", err)
	}
	hook, err := os.ReadFile(filepath.Join(adminPath, "hooks", "post-receive"))
	if err != nil {
		t.Fatalf("post-receive hook missing: %v", err)
	}
	if !strings.Contains(string(hook), fakeBin) || !strings.Contains(string(hook), "sync") {
		t.Errorf("unexpected post-receive hook:\n%s", hook)
	}

	cfgRaw, err := exec.Command("git", "-C", adminPath, "show", "HEAD:config.yaml").Output()
	if err != nil {
		t.Fatalf("config.yaml missing from admin repo HEAD: %v", err)
	}
	if !strings.Contains(string(cfgRaw), "alice") || !strings.Contains(string(cfgRaw), key) {
		t.Errorf("initial config does not contain the admin user:\n%s", cfgRaw)
	}

	if _, err := os.Stat(filepath.Join(home, ".repomouse", "config.yaml")); err != nil {
		t.Errorf("config cache not written: %v", err)
	}
	ak, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("authorized_keys not written: %v", err)
	}
	if !strings.Contains(string(ak), fakeBin+" shell alice") {
		t.Errorf("authorized_keys does not force the shell command:\n%s", ak)
	}
}

func TestSetup_refusesToOverwrite(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "repos", "admin.git"), 0750); err != nil {
		t.Fatal(err)
	}

	err := Setup([]string{"--admin-user", "alice", "--admin-key", "ssh-ed25519 AAAA alice"})
	if err == nil {
		t.Fatal("expected Setup to refuse an existing admin repo")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetup_requiresFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Setup(nil); err == nil {
		t.Fatal("expected error when --admin-user/--admin-key are missing")
	}
	if err := Setup([]string{"--admin-user", "alice"}); err == nil {
		t.Fatal("expected error when --admin-key is missing")
	}
}
