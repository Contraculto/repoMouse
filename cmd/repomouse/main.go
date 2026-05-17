package main

import (
	"fmt"
	"os"

	"contraculto.com/repomouse/internal/admin"
	"contraculto.com/repomouse/internal/prereceive"
	"contraculto.com/repomouse/internal/shell"
)

const usage = `repomouse — minimal git hosting over system SSH

Commands:
  setup --admin-user NAME --admin-key "ssh-..."   one-time server initialization
  sync                                             rebuild authorized_keys and ensure repos exist
  shell <username>                                 handle a git SSH session (used in authorized_keys)
  pre-receive <username>                           pre-receive hook: enforce RW vs RW+ (force push)

Typical flow:
  1. sudo -u git repomouse setup --admin-user alice --admin-key "ssh-ed25519 AAAA..."
  2. git clone git@yourhost:admin
  3. edit config.yaml, git push  →  sync runs automatically via post-receive hook
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "shell":
		if len(os.Args) < 3 {
			fatalf("shell: missing username")
		}
		err = shell.Run(os.Args[2])
	case "pre-receive":
		if len(os.Args) < 3 {
			fatalf("pre-receive: missing username")
		}
		err = prereceive.Run(os.Args[2])
	case "sync":
		err = admin.Sync()
	case "setup":
		err = admin.Setup(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "repomouse: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "repomouse: %v\n", err)
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "repomouse: "+format+"\n", args...)
	os.Exit(1)
}
