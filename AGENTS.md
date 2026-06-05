# repoMouse — Agent Orientation Guide

> Minimal self-hosted git hosting over system SSH. No daemon, no built-in SSH server — just OpenSSH + a single static binary.

---

## 1. Quickstart

```bash
cd /mnt/Sixto/DEV/repoMouse

# Build
go build ./cmd/repomouse

# Run tests
go test ./...

# Deploy binary to server
# (scp or use the deploy script)
```

---

## 2. Stack

| Layer | Choice |
|-------|--------|
| Language | Go 1.23+ |
| Auth | OpenSSH authorized_keys with forced commands |
| Config | YAML (managed via git push to admin repo) |
| License | MIT |

---

## 3. Project Structure

```
repoMouse/
├── cmd/repomouse/          # CLI entry point
├── internal/
│   ├── config/             # YAML parsing
│   ├── ssh/                # authorized_keys management
│   └── git/                # git command wrappers
├── go.mod, go.sum
└── README.md
```

---

## 4. How to Navigate

| Task | File(s) |
|------|---------|
| Add a CLI command | `cmd/repomouse/main.go` |
| Change config format | `internal/config/` |
| Change SSH key handling | `internal/ssh/` |
| Change git exec logic | `internal/git/` |

---

## 5. Dev Commands

```bash
go build ./cmd/repomouse
go test ./...
go vet ./...
```

---

## 6. Deploy

The binary is deployed to the server (contraculto.com) manually. The `admin` repo in `~/DEV/admin/` controls repoMouse configuration.

```bash
# After updating repoMouse binary, restart the forced-command handler
# (typically by updating the binary path in authorized_keys)
```

---

## 7. Critical Rules

1. **Never log or expose SSH private keys.** Only public keys are stored in config.
2. **The admin repo is the source of truth.** All repo/user changes go through `~/DEV/admin/config.yaml`.
3. **Keep the binary static.** No CGO dependencies if possible — makes deployment trivial.
4. **Validate YAML before writing.** A bad config locks everyone out until fixed server-side.
