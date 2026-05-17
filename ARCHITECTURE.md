# repomouse — architecture

## The core idea

repomouse does one thing: it sits between OpenSSH and git. OpenSSH handles all the crypto and authentication. Git handles all the repository logic. repomouse only does access control and routing between them.

No daemon runs. Every git operation — clone, push, pull — is a fresh process that starts, checks permissions, execs git, and exits. The server is stateless between operations.

---

## How SSH git hosting works in general

When you run `git clone user@host:repo`, git invokes SSH and asks it to run `git-upload-pack 'repo.git'` on the remote. The SSH server authenticates you, runs that command, and pipes the git protocol over the connection. That's it — git over SSH is just git commands running remotely over a pipe.

The traditional way to lock this down (gitolite's approach, which repomouse copies): add a `command=` option to `authorized_keys`. This tells OpenSSH "no matter what the client asks to run, run *this* instead." The client's requested command is stored in `$SSH_ORIGINAL_COMMAND` for the forced command to inspect.

So repomouse's `authorized_keys` looks like:

```
command="/usr/local/bin/repomouse shell alice",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA...
command="/usr/local/bin/repomouse shell bob",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA...
```

One line per key, per user. The options after `command=` strip every other SSH capability — no port forwarding, no terminal, no agent. The connection can only be used to run the forced command.

When alice pushes, OpenSSH matches her key, runs `repomouse shell alice`, and puts her actual request (`git-receive-pack 'myrepo.git'`) in `SSH_ORIGINAL_COMMAND`. repomouse checks if alice can write to myrepo, then `exec`s `git-receive-pack` — replacing itself with git in the same process.

---

## File by file

```
repomouse/
  cmd/repomouse/main.go          entry point
  internal/
    config/config.go             config parsing
    access/access.go             permission rule engine
    shell/shell.go               the hot path: SSH → git
    prereceive/prereceive.go     force-push hook
    admin/
      setup.go                   one-time initialization
      sync.go                    rebuild authorized_keys, create repos
```

---

### `cmd/repomouse/main.go`

Pure dispatch. Reads `os.Args[1]` and calls into the right package. No logic here — it just maps subcommands to functions and exits on error.

Four subcommands:
- `shell` — runs on every git operation (the hot path)
- `pre-receive` — runs inside a repo during push (the hook)
- `sync` — admin operation: rebuild authorized_keys and create repos
- `setup` — one-time initialization

---

### `internal/config/config.go`

Defines what a valid configuration looks like and how to load it.

**The config model:**

```
Config
 ├── ReposDir          where bare repos live on disk
 ├── Users             map[name → {admin bool, keys []string}]
 ├── Groups            map[name → []username]
 └── Repos             map[name → {Rules []Rule}]
                                     └── Rule: {Perm, Principals[]string}
```

**Permissions** are an enum: `PermDeny (-) < PermRead (R) < PermWrite (RW) < PermForce (RW+)`.

**Rules** are stored as `"RW+ = alice @devs"` in YAML. `Parse()` splits on `=`, reads the permission token on the left, splits the right side into individual principals (usernames or `@groups`). Parsing fails fast on unknown permissions, missing `=`, or empty principals — config errors should be loud.

**Two loading paths:**
- `Load(adminRepoPath)` — reads `config.yaml` from the bare admin repo using `git show HEAD:config.yaml`. Used by `sync`.
- `LoadCache(cachePath)` — reads from `~/.repomouse/config.yaml`. Used by `shell` and `pre-receive` on every git operation.

The split exists for performance and reliability. Reading from a bare git repo requires shelling out to git. That's fine during sync (happens once per config push), but you don't want it on every SSH connection. The cache is a plain file read — fast and with no extra process.

---

### `internal/access/access.go`

The rule engine. Pure function, no I/O.

```go
func Check(cfg *Config, username, repoName string, op Op) bool
```

`Op` is `OpRead`, `OpWrite`, or `OpForce`.

**How it evaluates:**

1. Look up the repo — if it doesn't exist in config, deny immediately.
2. Build the user's group memberships: always includes `@all`, plus any groups they appear in.
3. Walk the repo's rules in order. For each rule, check if any principal matches the username or one of their groups.
4. First match wins — return whether that rule's permission level covers the requested operation.
5. If no rule matches — deny.

**Permission levels** are cumulative upward: `R` covers reads, `RW` covers reads and non-force writes, `RW+` covers everything. `PermDeny` matches but always returns false, stopping evaluation.

This is identical to how gitolite works. The first-match-wins semantics mean you put specific rules before broad ones:

```yaml
- RW+ = alice          # alice first — gets force push
- RW  = @devs          # devs second — gets regular push
- R   = @all           # everyone else — read only
```

The admin repo (`admin`) is not in this config — it's handled as a special case directly in `shell.go`. Only users with `admin: true` can push to it. This keeps the config clean and prevents someone accidentally granting random users access to the admin repo via a config rule.

---

### `internal/shell/shell.go`

The hot path. This runs on every single git operation.

**Flow:**

1. Read `SSH_ORIGINAL_COMMAND`. If empty, someone is trying an interactive shell — print an error and exit. (The no-pty option in authorized_keys prevents a terminal, but the check is defense in depth.)

2. Parse the command with `parseCommand()`. This handles all four forms git uses:
   - `git-upload-pack 'repo.git'` (fetch/clone/pull)
   - `git upload-pack 'repo.git'` (alternate form)
   - `git-receive-pack 'repo.git'` (push)
   - `git receive-pack 'repo.git'` (alternate form)

   It strips single/double quotes, leading slashes, and `.git` suffix to get a clean repo name. Upload-pack maps to `OpRead`, receive-pack maps to `OpWrite`.

3. Load the config cache from `~/.repomouse/config.yaml`.

4. Check access: if the repo is `admin`, require `user.Admin == true`. Otherwise call `access.Check()`.

5. Stat the repo path on disk. If it doesn't exist, deny with a clear message. (A repo in config but not on disk means sync hasn't run yet, or someone edited config manually.)

6. Set `REPOMOUSE_USER` and `REPOMOUSE_REPO` as environment variables, then `syscall.Exec` the real git command.

**`syscall.Exec`** replaces the current process image entirely — repomouse is gone, git is now the process. This is important: it means git has full control of stdin/stdout for the binary git protocol, and there's no wrapper process sitting in the middle. It's the same file descriptors OpenSSH handed us, passed directly to git.

---

### `internal/prereceive/prereceive.go`

Handles the distinction between `RW` (push allowed, force push not) and `RW+` (force push allowed).

This runs as a git hook inside the repo, not directly from SSH. When `shell.go` execs `git-receive-pack`, git runs the repo's `hooks/pre-receive` script before accepting any new objects. That script is:

```sh
#!/bin/sh
exec repomouse pre-receive "$REPOMOUSE_USER"
```

`$REPOMOUSE_USER` was set by `shell.go` before exec, so it survives into the hook process.

Git passes old/new ref information on stdin, one line per ref being updated:

```
<old-sha> <new-sha> refs/heads/main
```

For each ref:
- If `old-sha` is all zeros — new branch, not a force push, always fine.
- If `new-sha` is all zeros — deleting a ref, requires `RW+`.
- Otherwise: run `git merge-base --is-ancestor old new`. If old is an ancestor of new, it's a fast-forward (normal push). If not, it's a rewrite of history — force push, requires `RW+`.

If the user doesn't have `OpForce` permission and a force push is detected, print an error and `os.Exit(1)`. A non-zero exit from pre-receive aborts the entire push. Git never writes the new objects to the repo.

---

### `internal/admin/sync.go`

Runs whenever the admin repo is pushed to, via the post-receive hook:

```sh
#!/bin/sh
exec repomouse sync
```

**What sync does:**

1. Shell out to `git -C /home/mouse/repos/admin.git show HEAD:config.yaml` to get the raw YAML bytes.
2. Parse them to validate — if the config is broken, fail loudly before touching anything.
3. Write the raw bytes to `~/.repomouse/config.yaml` (the cache). This is what `shell` and `pre-receive` read on every connection.
4. Rebuild `~/.ssh/authorized_keys`. Iterate all users sorted alphabetically (for deterministic diffs), generate one line per key with `command="repomouse shell <username>"` and all the restriction options.
5. Resolve `os.Executable()` and `filepath.EvalSymlinks()` to get the real binary path — important if repomouse is symlinked from `/usr/local/bin`.
6. Walk all repos in config. For each one that doesn't exist on disk, call `git init --bare` then `git symbolic-ref HEAD refs/heads/main` (so new clones default to the main branch), then install the `pre-receive` hook.

The key property: **sync is idempotent**. Running it twice changes nothing. Existing repos are not touched. authorized_keys is rewritten atomically (`os.WriteFile` is a single write). This means if something goes wrong mid-sync, the worst case is an inconsistent authorized_keys that gets fixed on the next sync.

---

### `internal/admin/setup.go`

One-time initialization. Takes `--admin-user` and `--admin-key` flags.

1. Create `~/repos/` directory.
2. `git init --bare ~/repos/admin.git`.
3. Install the post-receive hook in admin.git pointing to `repomouse sync`.
4. Generate an initial `config.yaml` with the admin user and commented-out examples.
5. Commit it: create a temp dir, `git init`, write the file, `git commit`, `git push adminPath HEAD:main` — then `git symbolic-ref HEAD refs/heads/main` in the bare repo so HEAD points correctly.
6. Call `Sync()` directly — this writes the cache, writes authorized_keys with the first key, and leaves the system in a fully operational state.

After setup, the admin user can immediately `git clone mouse@host:admin` and start editing config.

---

## The full lifecycle of a push

```
git push origin main
  └── SSH connects to contraculto.com as mouse
        └── OpenSSH matches key in authorized_keys
              └── runs: repomouse shell rodrigo
                    └── reads SSH_ORIGINAL_COMMAND=git-receive-pack 'myrepo.git'
                          └── parses → OpWrite, repo="myrepo"
                                └── loads ~/.repomouse/config.yaml
                                      └── access.Check(cfg, "rodrigo", "myrepo", OpWrite)
                                            └── rule "RW+ = rodrigo" matches → true
                                                  └── stat /home/mouse/repos/myrepo.git ✓
                                                        └── setenv REPOMOUSE_USER, REPOMOUSE_REPO
                                                              └── syscall.Exec git-receive-pack
                                                                    └── git runs pre-receive hook
                                                                          └── repomouse pre-receive rodrigo
                                                                                └── reads stdin refs
                                                                                      └── all fast-forward → exit 0
                                                                                            └── git writes objects
                                                                                                  └── push succeeds
```

And when you push to `admin`:

```
git push origin main   (admin clone)
  └── same SSH path → repomouse shell rodrigo
        └── repo="admin", user is admin → access granted
              └── syscall.Exec git-receive-pack /home/mouse/repos/admin.git
                    └── git runs post-receive hook
                          └── repomouse sync
                                └── reads new config.yaml from admin.git HEAD
                                      └── writes cache, rebuilds authorized_keys, creates new repos
                                            └── "sync complete"
```

---

## Why these choices

**System SSH over built-in SSH server** — OpenSSH is audited by the world and patched by your distro. A built-in SSH server (like gitdir uses) means you own that CVE surface and must update the library yourself.

**`syscall.Exec` instead of `exec.Command`** — exec.Command would fork a child process, leaving repomouse alive as a parent. Exec replaces the process entirely, which means git gets the real stdin/stdout file descriptors with no overhead or buffering in between. It's also how shells work.

**Config cache** — parsing YAML and shelling to git on every SSH connection would add latency and complexity. The cache is written atomically on every config push, so it's always consistent with the last successful sync.

**Admin repo in git** — configuration changes are auditable (git log), reversible (git revert), and require no server access to apply. The post-receive hook closes the loop between editing config and it taking effect.

**No database, no API, no web UI** — every piece of state is either a file on disk or a git object. You can inspect, back up, and migrate the entire system with standard unix tools.
