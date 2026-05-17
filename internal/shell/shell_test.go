package shell

import (
	"testing"

	"contraculto.com/repomouse/internal/access"
)

var parseCommandTests = []struct {
	name    string
	cmd     string
	wantOp  access.Op
	wantRepo string
	wantArgv []string
	wantErr  bool
}{
	{
		"git-upload-pack single quotes",
		"git-upload-pack 'myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack double quotes",
		`git-upload-pack "myrepo.git"`,
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack no .git suffix",
		"git-upload-pack 'myrepo'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git-upload-pack leading slash",
		"git-upload-pack '/myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"git upload-pack space form",
		"git upload-pack 'myrepo.git'",
		access.OpRead, "myrepo",
		[]string{"git", "upload-pack", "__REPO__"}, false,
	},
	{
		"git-receive-pack",
		"git-receive-pack 'myrepo.git'",
		access.OpWrite, "myrepo",
		[]string{"git-receive-pack", "__REPO__"}, false,
	},
	{
		"git receive-pack space form",
		"git receive-pack 'myrepo.git'",
		access.OpWrite, "myrepo",
		[]string{"git", "receive-pack", "__REPO__"}, false,
	},
	{
		"repo with dashes and underscores",
		"git-upload-pack 'my-cool_repo.git'",
		access.OpRead, "my-cool_repo",
		[]string{"git-upload-pack", "__REPO__"}, false,
	},
	{
		"unknown command",
		"git-daemon-export-ok",
		0, "", nil, true,
	},
	{
		"empty repo name",
		"git-upload-pack ''",
		0, "", nil, true,
	},
}

func TestParseCommand(t *testing.T) {
	for _, tt := range parseCommandTests {
		t.Run(tt.name, func(t *testing.T) {
			op, repo, argv, err := parseCommand(tt.cmd)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got op=%v repo=%q", op, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if op != tt.wantOp {
				t.Errorf("op = %v, want %v", op, tt.wantOp)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if len(argv) != len(tt.wantArgv) {
				t.Errorf("argv = %v, want %v", argv, tt.wantArgv)
				return
			}
			for i := range argv {
				if argv[i] != tt.wantArgv[i] {
					t.Errorf("argv[%d] = %q, want %q", i, argv[i], tt.wantArgv[i])
				}
			}
		})
	}
}
