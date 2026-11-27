# repoMouse

Mini mousey GIT repos for everymouse.

[![CI](https://github.com/Contraculto/repoMouse/actions/workflows/ci.yml/badge.svg)](https://github.com/Contraculto/repoMouse/actions/workflows/ci.yml)

A minimal self-hosted Git server. Allows you to keep several repos on your own server with multi user access using SSH public keys. A single binary, the only dependencies are openssh server and git.

You can have multiple users or groups with different levels of permission to each of your repos, with all the configuration done by pushing a single yaml configuration file to the admin repository.

`sync` only manages the `authorized_keys` lines it wrote (tagged `repoMouse:<user>`), so a personal login key on the same account keeps working.

## How it works

repoMouse uses OpenSSH for authentication — no built-in SSH server to audit or patch. Each user's public key is written into a system user's `authorized_keys` with a forced command (`repomouse shell <username>`). When you `git push`, OpenSSH handles the connection, repomouse checks permissions against the config, then hands off to the real git command.

```
you                     server (OpenSSH)           repomouse
 |                            |                        |
 |-- git push origin main --> |                        |
 |                            |-- repomouse shell ---> |
 |                            |                        |-- check perms
 |                            |                        |-- exec git-receive-pack
 |                            | <----- git protocol -- |
```

## Installation

### Requirements

- Linux server with `openssh-server` and `git`
- Go 1.22+ to build from source
- A system user to own the repos (conventionally `mouse`)

### Build and deploy

Use the deploy script — it builds a static binary, copies it to the server and
verifies it runs:

```bash
DEPLOY_HOST=yourserver.com ./deploy.sh          # DEPLOY_USER, DEPLOY_PATH are optional
```

Or build manually:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o repomouse ./cmd/repomouse
scp repomouse root@yourserver.com:/usr/local/bin/repomouse
```

### One-time server setup

```bash
# on the server, as root
useradd -m -s /bin/bash mouse

# as the repo-owning user — setup writes ~/repos and ~/.ssh for that user
sudo -u mouse repomouse setup --admin-user yourname --admin-key "ssh-ed25519 AAAA..."
```

This initializes `~/repos/admin.git` with a starter `config.yaml`, installs the post-receive hook, and writes the first `authorized_keys`. Output tells you where to go next.

## Configuration

Clone the admin repo and edit `config.yaml`:

```bash
git clone mouse@yourserver.com:admin
cd admin
# edit config.yaml
git push   # changes apply immediately via post-receive hook
```

### config.yaml

`server.repos_dir` must be an absolute path (defaults to `$HOME/repos`). Repo
names are validated — no `..`, no absolute paths — and `admin` is reserved for
the config repo.

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
| `-`   |      |      |                         |

A `-` rule explicitly denies the principal and stops rule evaluation for them.

Principals can be a username, a `@group` name, or `@all`.

### Admin access

Users with `admin: true` have implicit `RW+` on the `admin` repo. All other repos still require explicit rules.

### managed keys

`sync` owns only the `authorized_keys` lines it wrote — they end with a
`repoMouse:<username>` tag. Any other line in that file (your own login key,
keys for other tools) is left alone, so repoMouse can share the account. The
file is replaced atomically, so a crash cannot leave a truncated
`authorized_keys`.

## Commands

```
repomouse setup --admin-user NAME --admin-key "ssh-..."   one-time server initialization
repomouse sync                                            rebuild authorized_keys and ensure repos exist
repomouse repair-hooks                                    repoint repo hooks at the current binary
repomouse shell <username>                                handle a git SSH session (used in authorized_keys)
repomouse pre-receive <username>                          pre-receive hook: enforce RW vs RW+ (force push)
```

`sync` runs automatically on every push to the `admin` repo. Run `repair-hooks`
after moving the binary to a different path.

## Inspiration

repoMouse aims to serve a specific type of user trapped between just a bare git repo and installing a heavy solution like Gitea, it borrows heavily from [gitolite](https://github.com/sitaramc/gitolite) by Sitaram Chamarty, the original as far as I know and [gitdir](https://github.com/belak/gitdir) by Kaleb Elwert, which is a modern Go approach to the same problem.

This version is a simplified mix of ideas taken mostly from both of those and some other projects to cover the simplest use-case which is exactly what I need.

## License

MIT. See [LICENSE](LICENSE).
