package access

import "contraculto.com/repomouse/internal/config"

// Op is a git operation type.
type Op int

const (
	OpRead  Op = iota // git-upload-pack (fetch/clone/pull)
	OpWrite           // git-receive-pack, fast-forward only
	OpForce           // git-receive-pack, force push or ref delete
)

// Check returns true if username may perform op on repoName.
// Rules are evaluated in order; the first matching rule wins.
// If no rule matches, access is denied.
func Check(cfg *config.Config, username, repoName string, op Op) bool {
	repo, ok := cfg.Repos[repoName]
	if !ok {
		return false
	}
	groups := userGroups(cfg, username)
	for _, rule := range repo.Rules {
		if ruleMatches(rule, username, groups) {
			return permAllows(rule.Perm, op)
		}
	}
	return false
}

func permAllows(perm config.Permission, op Op) bool {
	switch perm {
	case config.PermDeny:
		return false
	case config.PermRead:
		return op == OpRead
	case config.PermWrite:
		return op == OpRead || op == OpWrite
	case config.PermForce:
		return true
	}
	return false
}

func ruleMatches(rule config.Rule, username string, groups map[string]bool) bool {
	for _, p := range rule.Principals {
		if p == username || groups[p] {
			return true
		}
	}
	return false
}

// userGroups returns the set of @group names the user belongs to, plus @all.
func userGroups(cfg *config.Config, username string) map[string]bool {
	m := map[string]bool{"@all": true}
	for group, members := range cfg.Groups {
		for _, member := range members {
			if member == username {
				m["@"+group] = true
				break
			}
		}
	}
	return m
}
