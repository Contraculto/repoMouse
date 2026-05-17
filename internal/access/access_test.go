package access

import (
	"testing"

	"contraculto.com/repomouse/internal/config"
)

func makeConfig() *config.Config {
	return &config.Config{
		Users: map[string]config.User{
			"alice": {Admin: true},
			"bob":   {},
			"carol": {},
		},
		Groups: map[string][]string{
			"devs":    {"alice", "bob"},
			"readers": {"carol"},
		},
		Repos: map[string]config.Repo{
			"proj": {Rules: []config.Rule{
				{Perm: config.PermForce, Principals: []string{"alice"}},
				{Perm: config.PermWrite, Principals: []string{"@devs"}},
				{Perm: config.PermRead, Principals: []string{"@readers"}},
			}},
			"locked": {Rules: []config.Rule{
				{Perm: config.PermDeny, Principals: []string{"bob"}},
				{Perm: config.PermWrite, Principals: []string{"@devs"}},
			}},
			"public": {Rules: []config.Rule{
				{Perm: config.PermRead, Principals: []string{"@all"}},
			}},
		},
	}
}

var checkTests = []struct {
	name   string
	user   string
	repo   string
	op     Op
	want   bool
}{
	// alice has RW+ on proj
	{"alice read proj", "alice", "proj", OpRead, true},
	{"alice write proj", "alice", "proj", OpWrite, true},
	{"alice force proj", "alice", "proj", OpForce, true},

	// bob is in @devs → RW on proj (first match is alice's RW+, bob doesn't match it)
	{"bob read proj", "bob", "proj", OpRead, true},
	{"bob write proj", "bob", "proj", OpWrite, true},
	{"bob force proj", "bob", "proj", OpForce, false},

	// carol is in @readers → R on proj
	{"carol read proj", "carol", "proj", OpRead, true},
	{"carol write proj", "carol", "proj", OpWrite, false},
	{"carol force proj", "carol", "proj", OpForce, false},

	// unknown user gets no match → denied
	{"unknown read proj", "dave", "proj", OpRead, false},
	{"unknown write proj", "dave", "proj", OpWrite, false},

	// unknown repo → denied
	{"alice unknown repo", "alice", "norepo", OpRead, false},

	// deny rule: bob is denied on locked before @devs rule matches
	{"bob denied on locked", "bob", "locked", OpRead, false},
	{"bob denied write on locked", "bob", "locked", OpWrite, false},
	// alice is NOT in the deny principal, falls through to @devs
	{"alice write locked", "alice", "locked", OpWrite, true},

	// @all: anyone can read public
	{"unknown read public", "dave", "public", OpRead, true},
	{"alice read public", "alice", "public", OpRead, true},
	// but @all R doesn't grant write
	{"unknown write public", "dave", "public", OpWrite, false},
}

func TestCheck(t *testing.T) {
	cfg := makeConfig()
	for _, tt := range checkTests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(cfg, tt.user, tt.repo, tt.op)
			if got != tt.want {
				t.Errorf("Check(%q, %q, %v) = %v, want %v",
					tt.user, tt.repo, tt.op, got, tt.want)
			}
		})
	}
}

func TestCheck_groupMembership(t *testing.T) {
	cfg := &config.Config{
		Users:  map[string]config.User{"alice": {}, "bob": {}},
		Groups: map[string][]string{"team": {"alice", "bob"}},
		Repos: map[string]config.Repo{
			"r": {Rules: []config.Rule{
				{Perm: config.PermWrite, Principals: []string{"@team"}},
			}},
		},
	}
	for _, u := range []string{"alice", "bob"} {
		if !Check(cfg, u, "r", OpWrite) {
			t.Errorf("%s should have write via @team", u)
		}
	}
	if Check(cfg, "carol", "r", OpWrite) {
		t.Error("carol should not have write, not in @team")
	}
}

func TestCheck_firstMatchWins(t *testing.T) {
	// alice appears in both a deny and a later RW+ — deny must win
	cfg := &config.Config{
		Users:  map[string]config.User{"alice": {}},
		Groups: map[string][]string{},
		Repos: map[string]config.Repo{
			"r": {Rules: []config.Rule{
				{Perm: config.PermDeny, Principals: []string{"alice"}},
				{Perm: config.PermForce, Principals: []string{"alice"}},
			}},
		},
	}
	if Check(cfg, "alice", "r", OpRead) {
		t.Error("alice should be denied by first rule")
	}
}
