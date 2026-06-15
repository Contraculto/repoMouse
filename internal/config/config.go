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

	for username := range cfg.Users {
		if err := ValidateName(username); err != nil {
			return nil, fmt.Errorf("user %q: %w", username, err)
		}
	}
	for groupName := range cfg.Groups {
		if err := ValidateName(groupName); err != nil {
			return nil, fmt.Errorf("group %q: %w", groupName, err)
		}
	}

	for username, user := range cfg.Users {
		for i, key := range user.Keys {
			if err := ValidateSSHKey(key); err != nil {
				return nil, fmt.Errorf("user %q key #%d: %w", username, i+1, err)
			}
		}
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

// ValidateName rejects user and group names that contain unsafe characters.
// Allowed characters are letters, digits, '_', '-', and '.'.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	for _, r := range name {
		if !isSafeNameRune(r) {
			return fmt.Errorf("name %q contains invalid character %q", name, r)
		}
	}
	return nil
}

func isSafeNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '-' || r == '.'
}

// ValidateRepoName rejects names that could escape repos_dir or are otherwise unsafe.
// Safe names may contain letters, digits, '_', '-', '.', and '/' for namespacing,
// but no path component may be "." or "..", and the name must not start with '/'.
func ValidateRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("empty repository name")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("repository name %q must not start with '/'", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" {
			return fmt.Errorf("repository name %q contains empty path component", name)
		}
		if part == "." || part == ".." {
			return fmt.Errorf("repository name %q contains %q", name, part)
		}
		for _, r := range part {
			if !isSafeRepoNameRune(r) {
				return fmt.Errorf("repository name %q contains invalid character %q", name, r)
			}
		}
	}
	return nil
}

func isSafeRepoNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '-' || r == '.'
}

// ValidateSSHKey rejects key strings that could corrupt authorized_keys or are malformed.
// It requires a single-line key starting with a recognized SSH public-key algorithm.
func ValidateSSHKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty key")
	}
	if strings.ContainsAny(key, "\n\r\t") {
		return fmt.Errorf("key contains forbidden whitespace")
	}
	fields := strings.Fields(key)
	if len(fields) < 2 {
		return fmt.Errorf("key must contain algorithm and data")
	}
	if !isKnownKeyAlgorithm(fields[0]) {
		return fmt.Errorf("unknown key algorithm %q", fields[0])
	}
	return nil
}

func isKnownKeyAlgorithm(algo string) bool {
	switch algo {
	case "ssh-ed25519", "ssh-rsa",
		"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com",
		"rsa-sha2-256", "rsa-sha2-512":
		return true
	}
	return false
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
