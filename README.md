# repomouse

Minimal self-hosted git hosting over system SSH. A couple of repos, a couple of users, no moving parts.

## How it works

repomouse uses OpenSSH for authentication — no built-in SSH server to audit or patch. Each user's public key is written into a system user's `authorized_keys` with a forced command (`repomouse shell <username>`). When you `git push`, OpenSSH handles the connection, repomouse checks permissions against the config, then hands off to the real git command.

Configuration lives in a git repository (`admin`) on the server itself. Push a change to `admin` → a post-receive hook rebuilds `authorized_keys` and creates any new repos. No server restarts, no files to hand-edit on the server.

```
you                     server (OpenSSH)           repomouse
 |                            |                        |
 |-- git push origin main --> |                        |
 |                            |-- repomouse shell ---> |
 |                            |                        |-- check perms
 |                            |                        |-- exec git-receive-pack
 |                            | <----- git protocol -- |
```

## Features

- No daemon — each git operation is a fresh SSH process
- Single static binary, only runtime dependency is `git`
- Config managed via git push to an `admin` repo
- `R` / `RW` / `RW+` permissions per repo, with groups and `@all`
- Force-push and ref-delete controlled separately from regular push
- Repos created automatically when added to config

## Installation

### Requirements

- Linux server with `openssh-server` and `git`
- Go 1.22+ to build from source
- A system user to own the repos (conventionally `mouse` or `git`)

### Build and deploy

```bash
DEPLOY_HOST=yourserver.com ./deploy.sh
```

Or manually:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o repomouse ./cmd/repomouse
scp repomouse root@yourserver.com:/usr/local/bin/repomouse
```

### One-time server setup

```bash
# on the server, as root
useradd -m -s /bin/bash mouse
repomouse setup --admin-user yourname --admin-key "ssh-ed25519 AAAA..."
```

This initializes `~/repos/admin.git` with a starter `config.yaml`, installs the post-receive hook, and writes the first `authorized_keys`. Output tells you where to go next.

### SSH client (optional convenience)

Add to `~/.ssh/config` on your local machine:

```
Host yourserver.com
    User mouse
```

Then `git clone yourserver.com:reponame` works. Without it, use the full form: `git clone mouse@yourserver.com:reponame`.

## Configuration

Clone the admin repo and edit `config.yaml`:

```bash
git clone mouse@yourserver.com:admin
cd admin
# edit config.yaml
git push   # changes apply immediately via post-receive hook
```

### config.yaml

```yaml
# optional — defaults to $HOME/repos
server:
  repos_dir: /home/mouse/repos

users:
  alice:
    admin: true          # can push to the admin repo
    keys:
      - "ssh-ed25519 AAAA... alice@laptop"
      - "ssh-ed25519 AAAA... alice@work"   # multiple keys per user OK
  bob:
    keys:
      - "ssh-ed25519 AAAA... bob@desktop"

groups:
  devs: [alice, bob]

repos:
  myproject:
    rules:
      - RW+ = alice      # alice: full control including force push
      - RW  = @devs      # devs: push, but no force push
      - R   = @all       # everyone else: read-only
```

### Permission rules

Rules are checked top-to-bottom. The first rule that matches the user wins. If no rule matches, access is denied.

| Rule  | Read | Push | Force push / delete ref |
|-------|:----:|:----:|:-----------------------:|
| `R`   |  ✓   |      |                         |
| `RW`  |  ✓   |  ✓   |                         |
| `RW+` |  ✓   |  ✓   |  ✓                      |
| `-`   | explicit deny on first match  |

Principals can be a username, a `@group` name, or `@all`.

### Admin access

Users with `admin: true` have implicit `RW+` on the `admin` repo. All other repos still require explicit rules.

## Inspiration

repomouse started from frustration with the gap between "too simple" (bare git + manual authorized_keys) and "too much" (Gitea, GitLab) for a use case that's really just a couple of repos and a handful of trusted users.

Two projects shaped its design directly:

**[gitolite](https://github.com/sitaramc/gitolite)** by Sitaram Chamarty — the gold standard for SSH git hosting. repomouse borrows its permission model (`R`/`RW`/`RW+`), group syntax (`@group`, `@all`), and admin-repo-as-config approach directly from gitolite. Gitolite is mature, featureful, and battle-tested across many large deployments; repomouse is a deliberately narrow subset of what it can do, without the Perl dependency.

**[gitdir](https://github.com/belak/gitdir)** by Kaleb Elwert — a Go reimplementation of gitolite-style hosting. repomouse takes its single-binary goal and YAML config aesthetic from gitdir, but opts for system SSH over a built-in SSH server to reduce the security surface.

Both projects have seen little activity in recent years. repomouse aims to stay current with modern Go and remain small enough to be fully understood and maintained by its users.

## License

GPLv3. See [LICENSE](LICENSE).
