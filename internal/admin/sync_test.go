package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"contraculto.com/repomouse/internal/config"
)

func testConfig(t *testing.T, reposDir string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(`
server:
  repos_dir: ` + reposDir + `
users:
  zoe:
    admin: true
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID zoe@laptop"
  alice:
    admin: false
    keys:
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"
      - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@work"
repos:
  myrepo:
    rules:
      - "RW+ = alice"
`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRebuildAuthorizedKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := testConfig(t, filepath.Join(home, "repos"))
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	akPath := filepath.Join(home, ".ssh", "authorized_keys")
	info, err := os.Stat(akPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("authorized_keys mode = %o, want 600", perm)
	}

	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	want := []string{
		`command="/usr/local/bin/repomouse shell alice",restrict,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop repoMouse:alice`,
		`command="/usr/local/bin/repomouse shell alice",restrict,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@work repoMouse:alice`,
		`command="/usr/local/bin/repomouse shell zoe",restrict,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID zoe@laptop repoMouse:zoe`,
	}
	for _, line := range want {
		if !strings.Contains(content, line+"\n") {
			t.Errorf("missing line %q in authorized_keys:\n%s", line, content)
		}
	}

	// Sorted by username for deterministic diffs: alice's two keys before zoe's.
	if strings.Index(content, "alice@laptop") > strings.Index(content, "zoe@laptop") {
		t.Errorf("authorized_keys not sorted by username:\n%s", content)
	}
}

func TestRebuildAuthorizedKeys_skipsEmptyKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// config.Parse rejects empty keys, but rebuildAuthorizedKeys must still be
	// defensive: a blank entry must not produce a broken authorized_keys line.
	cfg := &config.Config{Users: map[string]config.User{
		"alice": {Keys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop", "   "}},
	}}
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Header comment + one key.
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2:\n%s", len(lines), data)
	}
}

func TestEnsureRepos_createsBareReposWithHooks(t *testing.T) {
	home := t.TempDir()
	reposDir := filepath.Join(home, "repos")
	t.Setenv("HOME", home)

	cfg := testConfig(t, reposDir)
	if err := ensureRepos(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("ensureRepos: %v", err)
	}

	repoPath := filepath.Join(reposDir, "myrepo.git")
	head, err := os.ReadFile(filepath.Join(repoPath, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(head)) != "ref: refs/heads/main" {
		t.Errorf("HEAD = %q, want ref: refs/heads/main", head)
	}

	hook, err := os.ReadFile(filepath.Join(repoPath, "hooks", "pre-receive"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hook), "/usr/local/bin/repomouse") ||
		!strings.Contains(string(hook), "pre-receive") {
		t.Errorf("unexpected pre-receive hook:\n%s", hook)
	}
	info, err := os.Stat(filepath.Join(repoPath, "hooks", "pre-receive"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0755 {
		t.Errorf("hook mode = %o, want 755", perm)
	}

	// Idempotent: a second run must not fail or recreate anything.
	if err := ensureRepos(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("second ensureRepos: %v", err)
	}
}

func TestEnsureRepos_leavesExistingReposAlone(t *testing.T) {
	home := t.TempDir()
	reposDir := filepath.Join(home, "repos")
	t.Setenv("HOME", home)

	repoPath := filepath.Join(reposDir, "myrepo.git")
	if err := os.MkdirAll(repoPath, 0750); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(repoPath, "marker")
	if err := os.WriteFile(marker, []byte("keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, reposDir)
	if err := ensureRepos(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("ensureRepos: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing repo was modified: %v", err)
	}
}

func TestEnsureRepos_rejectsUnsafeNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.Config{
		ReposDir: filepath.Join(home, "repos"),
		Repos:    map[string]config.Repo{"../escape": {}},
	}
	if err := ensureRepos(cfg, "/usr/local/bin/repomouse"); err == nil {
		t.Fatal("expected error for unsafe repo name")
	}
	if _, err := os.Stat(filepath.Join(home, "escape.git")); err == nil {
		t.Error("repo was created outside repos_dir")
	}
}

// Verify Sync end-to-end against a real admin repo.
func TestSync_writesCacheKeysAndRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	reposDir := filepath.Join(home, "repos")
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
	if err := Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	cache, err := os.ReadFile(filepath.Join(home, ".repomouse", "config.yaml"))
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if strings.TrimSpace(string(cache)) != strings.TrimSpace(cfgYAML) {
		t.Errorf("cache does not match config:\n%s", cache)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); err != nil {
		t.Errorf("authorized_keys not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reposDir, "myrepo.git", "HEAD")); err != nil {
		t.Errorf("repo not created: %v", err)
	}
}

func TestSync_failsOnInvalidConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	reposDir := filepath.Join(home, "repos")
	adminPath := filepath.Join(reposDir, "admin.git")
	if err := os.MkdirAll(reposDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "--bare", adminPath).Run(); err != nil {
		t.Fatal(err)
	}
	if err := commitInitialConfig(adminPath, "repos:\n  myrepo:\n    rules:\n      - \"XX = alice\"\n"); err != nil {
		t.Fatalf("commitInitialConfig: %v", err)
	}
	if err := Sync(); err == nil {
		t.Fatal("expected Sync to fail on invalid config")
	}
	// Nothing should be touched when the config is broken.
	if _, err := os.Stat(filepath.Join(home, ".repomouse", "config.yaml")); err == nil {
		t.Error("cache was written despite invalid config")
	}
}

func TestRebuildAuthorizedKeys_preservesUnmanagedKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	existing := strings.Join([]string{
		"# managed by repomouse — do not edit manually", // legacy lowercase header
		`command="/usr/local/bin/repomouse shell alice",restrict,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@old repoMouse:alice`,
		`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID admin@laptop`,
		"# personal note left by a human",
		`from="10.0.0.0/8" ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ== backup@nas`,
		"",
	}, "\n")
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, filepath.Join(home, "repos"))
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, line := range []string{
		`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID admin@laptop`,
		"# personal note left by a human",
		`from="10.0.0.0/8" ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ== backup@nas`,
	} {
		if !strings.Contains(content, line+"\n") {
			t.Errorf("unmanaged line %q was dropped:\n%s", line, content)
		}
	}
	if strings.Contains(content, "alice@old") {
		t.Errorf("stale managed line was kept:\n%s", content)
	}
	if n := strings.Count(content, "# managed by repoMouse"); n != 1 {
		t.Errorf("header appears %d times, want 1:\n%s", n, content)
	}
	if !strings.Contains(content, "alice@laptop repoMouse:alice") {
		t.Errorf("managed line missing from output:\n%s", content)
	}
}

func TestRebuildAuthorizedKeys_replacesLegacyManagedLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Pre-tag line from an older version: only the binary basename identifies it.
	legacy := `command="/old/place/repomouse shell alice",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop
`
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, filepath.Join(home, "repos"))
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "/old/place/repomouse") {
		t.Errorf("legacy managed line was kept:\n%s", content)
	}
	if !strings.Contains(content, "alice@laptop repoMouse:alice") {
		t.Errorf("managed line not regenerated:\n%s", content)
	}
}

func TestRebuildAuthorizedKeys_replacesRenamedBinaryLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Tagged line pointing at a differently named binary: the tag identifies it.
	renamed := `command="/opt/tools/gitd shell alice",restrict,no-pty ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop repoMouse:alice
`
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte(renamed), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, filepath.Join(home, "repos"))
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "/opt/tools/gitd") {
		t.Errorf("stale line for renamed binary was kept:\n%s", content)
	}
	if !strings.Contains(content, "/usr/local/bin/repomouse shell alice") {
		t.Errorf("managed line not regenerated:\n%s", content)
	}
}

func TestRebuildAuthorizedKeys_leavesNoTempFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := testConfig(t, filepath.Join(home, "repos"))
	for i := 0; i < 3; i++ {
		if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
			t.Fatalf("rebuildAuthorizedKeys: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(home, ".ssh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".repomouse-tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func mustParse(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

func TestHookScriptsSurviveHostilePaths(t *testing.T) {
	// A binary path the shell would mangle if it were not quoted: $ expansion,
	// command substitution, spaces and a quote.
	const exe = "/opt/my repo/$HOME/`id`/it's/repomouse"

	for _, hook := range []string{buildPreReceiveHook(exe), buildPostReceiveHook(exe)} {
		if !strings.HasPrefix(hook, "#!/bin/sh\nexec '") {
			t.Errorf("hook not shell-quoted:\n%s", hook)
		}
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte(buildPostReceiveHook(exe)), 0755); err != nil {
		t.Fatal(err)
	}

	// /bin/sh -n parses without executing; an unquoted path would still parse,
	// so compare what the shell actually expands the exec target to.
	out, err := exec.Command("/bin/sh", "-c", `sed -n 's/^exec //p' "`+script+`"`).Output()
	if err != nil {
		t.Fatalf("reading hook: %v", err)
	}
	args, err := exec.Command("/bin/sh", "-c", `set -- `+strings.TrimSuffix(string(out), "\n")+`; printf '%s\n' "$1"`).Output()
	if err != nil {
		t.Fatalf("expanding hook command: %v", err)
	}
	if got := strings.TrimSuffix(string(args), "\n"); got != exe {
		t.Errorf("shell expands hook target to %q, want %q", got, exe)
	}
}

func TestCommandQuote(t *testing.T) {
	// OpenSSH understands only \" and \\ inside command="..."; everything else
	// must be passed through literally.
	tests := []struct{ in, want string }{
		{"/usr/local/bin/repomouse shell alice", `"/usr/local/bin/repomouse shell alice"`},
		{`/opt/a"b/repomouse`, `"/opt/a\"b/repomouse"`},
		{`/opt/back\slash/repomouse`, `"/opt/back\\slash/repomouse"`},
		{"/opt/ünïcode/repomouse", `"/opt/ünïcode/repomouse"`},
		{"/opt/dollar$HOME/repomouse", `"/opt/dollar$HOME/repomouse"`},
	}
	for _, tt := range tests {
		if got := commandQuote(tt.in); got != tt.want {
			t.Errorf("commandQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestIsManagedLine(t *testing.T) {
	const exe = `/opt/odd"path/repomouse`
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "current binary",
			line: "command=" + commandQuote(exe+" shell alice") + ",restrict,no-pty " + aliceKeyFixture + " repoMouse:alice",
			want: true,
		},
		{
			name: "renamed binary, tagged",
			line: `command="/opt/tools/gitd shell alice",restrict,no-pty ` + aliceKeyFixture + " repoMouse:alice",
			want: true,
		},
		{
			name: "legacy untagged, repomouse basename",
			line: `command="/old/place/repomouse shell alice",no-pty ` + aliceKeyFixture,
			want: true,
		},
		{
			name: "personal key whose comment mentions the tag",
			line: aliceKeyFixture + " repoMouse:alice",
			want: false,
		},
		{
			name: "someone else's forced command",
			line: `command="/usr/local/bin/other shell alice",restrict ` + aliceKeyFixture,
			want: false,
		},
		{
			name: "truncated forced command",
			line: `command="/usr/local/bin/repomouse shell alice,restrict ` + aliceKeyFixture,
			want: false,
		},
	}
	for _, tt := range tests {
		if got := isManagedLine(tt.line, exe); got != tt.want {
			t.Errorf("%s: isManagedLine = %v, want %v", tt.name, got, tt.want)
		}
	}
}

const aliceKeyFixture = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID alice@laptop"

func TestRebuildAuthorizedKeys_dropsLegacyHeaderOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	// An old file: legacy header at the top and a second stale header left at the
	// bottom by a previous merge, plus one personal key.
	existing := strings.Join([]string{
		"# managed by repomouse — do not edit manually",
		`command="/usr/local/bin/repomouse shell alice",restrict,no-pty ` + aliceKeyFixture + ` repoMouse:alice`,
		"",
		"# managed by repomouse — do not edit manually",
		`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDpersonal rod@laptop`,
		"",
	}, "\n")
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, filepath.Join(home, "repos"))
	if err := rebuildAuthorizedKeys(cfg, "/usr/local/bin/repomouse"); err != nil {
		t.Fatalf("rebuildAuthorizedKeys: %v", err)
	}

	data, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.ToLower(string(data)), "# managed by repomouse"); n != 1 {
		t.Errorf("header appears %d times, want 1:\n%s", n, data)
	}
	if !strings.Contains(string(data), "rod@laptop") {
		t.Errorf("personal key was dropped:\n%s", data)
	}
}
