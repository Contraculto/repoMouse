package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Permission represents an access level.
type Permission int

const (
	PermUnknown Permission = iota
	PermDeny              // -
	PermRead              // R
	PermWrite             // RW
	PermForce             // RW+
)

// Rule is a parsed access rule.
type Rule struct {
	Perm       Permission
	Principals []string // usernames or @groups
}

// User holds a user's admin status and SSH public keys.
type User struct {
	Admin bool     `yaml:"admin"`
	Keys  []string `yaml:"keys"`
}

// Repo holds the parsed rules for a repository.
type Repo struct {
	Rules []Rule
}

// Config is the fully parsed configuration.
type Config struct {
	ReposDir string
	Users    map[string]User
	Groups   map[string][]string
	Repos    map[string]Repo
}

type rawConfig struct {
	Server struct {
		ReposDir string `yaml:"repos_dir"`
	} `yaml:"server"`
	Users  map[string]User         `yaml:"users"`
	Groups map[string][]string     `yaml:"groups"`
	Repos  map[string]struct {
		Rules []string `yaml:"rules"`
	} `yaml:"repos"`
}

// Parse parses raw YAML config bytes into a Config.
func Parse(data []byte) (*Config, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	reposDir := raw.Server.ReposDir
	if reposDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		reposDir = filepath.Join(home, "repos")
	}

	cfg := &Config{
		ReposDir: reposDir,
		Users:    raw.Users,
		Groups:   raw.Groups,
		Repos:    make(map[string]Repo, len(raw.Repos)),
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]User)
	}
	if cfg.Groups == nil {
		cfg.Groups = make(map[string][]string)
	}

	for name, r := range raw.Repos {
		repo, err := parseRepo(name, r.Rules)
		if err != nil {
			return nil, err
		}
		cfg.Repos[name] = repo
	}
	return cfg, nil
}

func parseRepo(name string, rawRules []string) (Repo, error) {
	var repo Repo
	for _, s := range rawRules {
		rule, err := parseRule(s)
		if err != nil {
			return Repo{}, fmt.Errorf("repo %q rule %q: %w", name, s, err)
		}
		repo.Rules = append(repo.Rules, rule)
	}
	return repo, nil
}

func parseRule(s string) (Rule, error) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return Rule{}, fmt.Errorf("missing '='")
	}
	perm, err := parsePermission(strings.TrimSpace(s[:idx]))
	if err != nil {
		return Rule{}, err
	}
	principals := strings.Fields(s[idx+1:])
	if len(principals) == 0 {
		return Rule{}, fmt.Errorf("no principals")
	}
	return Rule{Perm: perm, Principals: principals}, nil
}

func parsePermission(s string) (Permission, error) {
	switch s {
	case "RW+":
		return PermForce, nil
	case "RW":
		return PermWrite, nil
	case "R":
		return PermRead, nil
	case "-":
		return PermDeny, nil
	default:
		return PermUnknown, fmt.Errorf("unknown permission %q", s)
	}
}

// AdminRepoPath returns the default path to the admin bare repo.
func AdminRepoPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "repos", "admin.git"), nil
}

// CachePath returns where sync caches the raw config YAML.
func CachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".repomouse", "config.yaml"), nil
}

// RepoPath returns the full filesystem path to a repo's bare git dir.
func RepoPath(reposDir, name string) string {
	return filepath.Join(reposDir, name+".git")
}

// ReadRaw extracts config.yaml from HEAD of the admin bare repo.
func ReadRaw(adminRepoPath string) ([]byte, error) {
	out, err := exec.Command("git", "-C", adminRepoPath, "show", "HEAD:config.yaml").Output()
	if err != nil {
		return nil, fmt.Errorf("reading config from %s: %w", adminRepoPath, err)
	}
	return out, nil
}

// Load reads and parses config from the admin bare repo.
// Returns both the raw bytes (for caching) and the parsed config.
func Load(adminRepoPath string) ([]byte, *Config, error) {
	data, err := ReadRaw(adminRepoPath)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	return data, cfg, nil
}

// LoadCache reads and parses the cached config file written by sync.
func LoadCache(cachePath string) (*Config, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading config cache %s: %w", cachePath, err)
	}
	return Parse(data)
}
