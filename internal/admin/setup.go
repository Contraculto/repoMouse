package admin

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"contraculto.com/repomouse/internal/config"
)

// Setup runs one-time server initialization.
// It creates the repos directory, initializes the admin bare repo with an
// initial config.yaml, installs the post-receive hook, and runs the first sync.
func Setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	adminUser := fs.String("admin-user", "", "admin username (required)")
	adminKey := fs.String("admin-key", "", "admin SSH public key (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *adminUser == "" || *adminKey == "" {
		return fmt.Errorf("--admin-user and --admin-key are required\n\nExample:\n  repomouse setup --admin-user alice --admin-key \"ssh-ed25519 AAAA...\"")
	}

	adminPath, err := config.AdminRepoPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(adminPath); err == nil {
		return fmt.Errorf("admin repo already exists at %s\nRun 'repomouse sync' to apply config changes", adminPath)
	}

	if err := os.MkdirAll(filepath.Dir(adminPath), 0750); err != nil {
		return fmt.Errorf("creating repos dir: %w", err)
	}
	fmt.Fprintf(os.Stderr, "repomouse: created %s\n", filepath.Dir(adminPath))

	if err := exec.Command("git", "init", "--bare", adminPath).Run(); err != nil {
		return fmt.Errorf("initializing admin repo: %w", err)
	}
	fmt.Fprintf(os.Stderr, "repomouse: initialized admin repo at %s\n", adminPath)

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	// post-receive hook: runs sync on every push to admin
	hookPath := filepath.Join(adminPath, "hooks", "post-receive")
	hook := fmt.Sprintf("#!/bin/sh\nexec %q sync\n", exePath)
	if err := os.WriteFile(hookPath, []byte(hook), 0755); err != nil {
		return fmt.Errorf("installing post-receive hook: %w", err)
	}

	initialConfig := buildInitialConfig(*adminUser, *adminKey)
	if err := commitInitialConfig(adminPath, initialConfig); err != nil {
		return fmt.Errorf("committing initial config: %w", err)
	}
	fmt.Fprintln(os.Stderr, "repomouse: committed initial config.yaml")

	if err := Sync(); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

	fmt.Fprintf(os.Stderr, `
repomouse: setup complete!

Clone the admin repo to manage users, groups, and repositories:
  git clone git@<your-host>:admin

Edit config.yaml and push to apply changes immediately.
`)
	return nil
}

func buildInitialConfig(adminUser, adminKey string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# repomouse config\n")
	fmt.Fprintf(&sb, "# Edit and push to apply changes.\n\n")
	fmt.Fprintf(&sb, "users:\n")
	fmt.Fprintf(&sb, "  %s:\n", adminUser)
	fmt.Fprintf(&sb, "    admin: true\n")
	fmt.Fprintf(&sb, "    keys:\n")
	fmt.Fprintf(&sb, "      - %q\n\n", adminKey)
	fmt.Fprintf(&sb, "groups:\n")
	fmt.Fprintf(&sb, "  # devs: [%s, bob]\n\n", adminUser)
	fmt.Fprintf(&sb, "repos:\n")
	fmt.Fprintf(&sb, "  # example:\n")
	fmt.Fprintf(&sb, "  #   rules:\n")
	fmt.Fprintf(&sb, "  #     - RW+ = %s\n", adminUser)
	fmt.Fprintf(&sb, "  #     - R   = @all\n")
	return sb.String()
}

// commitInitialConfig creates a temporary working repo, commits configYAML
// to it, then pushes to the bare admin repo.
func commitInitialConfig(adminPath, configYAML string) error {
	tmp, err := os.MkdirTemp("", "repomouse-init-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	run := func(args ...string) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmp
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), []byte(configYAML), 0644); err != nil {
		return err
	}

	steps := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "repomouse@localhost"},
		{"git", "config", "user.name", "repomouse"},
		{"git", "add", "config.yaml"},
		{"git", "commit", "-m", "initial config"},
		{"git", "push", adminPath, "HEAD:main"},
	}
	for _, s := range steps {
		if err := run(s...); err != nil {
			return fmt.Errorf("%v: %w", s, err)
		}
	}
	// Point HEAD to the branch we just pushed so git show HEAD:... works.
	return exec.Command("git", "-C", adminPath, "symbolic-ref", "HEAD", "refs/heads/main").Run()
}
