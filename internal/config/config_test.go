package config

import (
	"strings"
	"testing"
)

func TestParse_valid(t *testing.T) {
	yaml := `
server:
  repos_dir: /srv/repos
users:
  alice:
    admin: true
    keys:
      - "ssh-ed25519 AAAA alice"
  bob:
    keys:
      - "ssh-ed25519 AAAA bob"
groups:
  devs: [alice, bob]
repos:
  myrepo:
    rules:
      - "RW+ = alice"
      - "RW  = @devs"
      - "R   = @all"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ReposDir != "/srv/repos" {
		t.Errorf("ReposDir = %q, want /srv/repos", cfg.ReposDir)
	}
	if !cfg.Users["alice"].Admin {
		t.Error("alice should be admin")
	}
	if cfg.Users["bob"].Admin {
		t.Error("bob should not be admin")
	}
	if len(cfg.Groups["devs"]) != 2 {
		t.Errorf("devs group len = %d, want 2", len(cfg.Groups["devs"]))
	}

	repo, ok := cfg.Repos["myrepo"]
	if !ok {
		t.Fatal("myrepo not found")
	}
	if len(repo.Rules) != 3 {
		t.Fatalf("myrepo rules len = %d, want 3", len(repo.Rules))
	}
	if repo.Rules[0].Perm != PermForce {
		t.Errorf("rule[0] perm = %v, want RW+", repo.Rules[0].Perm)
	}
	if repo.Rules[1].Perm != PermWrite {
		t.Errorf("rule[1] perm = %v, want RW", repo.Rules[1].Perm)
	}
	if repo.Rules[2].Perm != PermRead {
		t.Errorf("rule[2] perm = %v, want R", repo.Rules[2].Perm)
	}
	if repo.Rules[1].Principals[0] != "@devs" {
		t.Errorf("rule[1] principal = %q, want @devs", repo.Rules[1].Principals[0])
	}
}

func TestParse_defaultReposDir(t *testing.T) {
	cfg, err := Parse([]byte("users: {}\nrepos: {}\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(cfg.ReposDir, "/repos") {
		t.Errorf("ReposDir = %q, want suffix /repos", cfg.ReposDir)
	}
}

func TestParse_emptyConfig(t *testing.T) {
	cfg, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Users == nil {
		t.Error("Users should not be nil")
	}
	if cfg.Groups == nil {
		t.Error("Groups should not be nil")
	}
}

func TestParse_multiplePrincipals(t *testing.T) {
	yaml := `
repos:
  r:
    rules:
      - "RW+ = alice bob @devs"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Repos["r"].Rules[0]
	if len(rule.Principals) != 3 {
		t.Errorf("principals len = %d, want 3", len(rule.Principals))
	}
}

var invalidRuleTests = []struct {
	name string
	yaml string
}{
	{
		"unknown permission",
		"repos:\n  r:\n    rules:\n      - \"XX = alice\"\n",
	},
	{
		"missing equals",
		"repos:\n  r:\n    rules:\n      - \"RW alice\"\n",
	},
	{
		"no principals",
		"repos:\n  r:\n    rules:\n      - \"RW = \"\n",
	},
}

func TestParse_invalidRules(t *testing.T) {
	for _, tt := range invalidRuleTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
