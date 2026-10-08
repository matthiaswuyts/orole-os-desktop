package files

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitListingDoesNotExecuteRepositoryFilters(t *testing.T) {
	for _, filter := range []string{"clean", "process"} {
		t.Run(filter, func(t *testing.T) {
			_, root := newRoot(t)
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
			}
			git("init")
			if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(root, ".gitattributes"), "*.txt filter=hostile\n")
			write(t, filepath.Join(root, "nested", "tracked.txt"), "before\n")
			git("add", ".")
			git("commit", "-m", "base")
			write(t, filepath.Join(root, "nested", "tracked.txt"), "after\n")
			write(t, filepath.Join(root, "nested", "new.txt"), "new\n")
			marker := filepath.Join(t.TempDir(), "executed")
			command := "echo executed > '" + strings.ReplaceAll(filepath.ToSlash(marker), "'", "'\\''") + "'; exit 1"
			git("config", "core.fsmonitor", command)
			git("config", "filter.hostile."+filter, command)
			git("config", "filter.hostile.required", "true")
			provider := NewGit(StaticRoots("git", root))
			entries, err := provider.List(ctx, filepath.Join(root, "nested"))
			if err != nil {
				t.Fatal(err)
			}
			statuses := make(map[string]string)
			for _, entry := range entries {
				statuses[entry.Name] = entry.Git
			}
			if statuses["tracked.txt"] != " M" || statuses["new.txt"] != "??" {
				t.Fatalf("listing lost real change annotations: %+v", statuses)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("repository command executed or marker unreadable: %v", err)
			}
		})
	}
}
