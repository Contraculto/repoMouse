package access

import "contraculto.com/repomouse/internal/config"

// AdminRepo is the repo that holds config.yaml. It is not listed in the config
// itself — access is granted only to users with admin: true.
const AdminRepo = config.AdminRepoName

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

// CheckRepo is Check, with the admin repo handled as a special case: it is not
// listed in the config, so it is allowed only for users with admin: true.
// Both the SSH hot path and the pre-receive hook use this, so admins keep
// force-push rights on the admin repo.
func CheckRepo(cfg *config.Config, username, repoName string, op Op) bool {
	if repoName == AdminRepo {
		u, ok := cfg.Users[username]
		return ok && u.Admin
	}
	return Check(cfg, username, repoName, op)
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
