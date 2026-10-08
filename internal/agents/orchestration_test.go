package agents_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
)

func TestJournalPreventsDuplicateDispatch(t *testing.T) {
	e := newEnv(t)
	o := orchestrate.New(e.m, nil)
	defer o.Shutdown()
	s := e.start(t, agents.StartSpec{Prompt: "first", Key: "start-once"})
	e.wait(t, s.ID, "done", status(agents.StatusDone))
	second := e.start(t, agents.StartSpec{Prompt: "different", Key: "start-once"})
	if second.ID != s.ID {
		t.Fatal("start dispatched twice")
	}
	for range 2 {
		if _, err := o.SendPrompt(ctx10(t), "prompt-once", s.ID, "second"); err != nil {
			t.Fatal(err)
		}
	}
	e.wait(t, s.ID, "second turn", func(s agents.Session) bool { return s.Turns == 2 && s.Status == agents.StatusDone })
	tr, _ := e.m.Transcript(s.ID, 100)
	if kinds(tr)[agents.EvPrompt] != 2 {
		t.Fatalf("duplicate prompt: %+v", tr)
	}
	_, _, err := e.st.JournalBegin("interrupted", "send-prompt", nil, "previous-boot")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.st.MarkInterrupted(e.m.Boot()); err != nil {
		t.Fatal(err)
	}
	if _, err = o.SendPrompt(ctx10(t), "interrupted", s.ID, "must not dispatch"); !errors.Is(err, orchestrate.ErrInterrupted) {
		t.Fatalf("got %v", err)
	}
	tr, _ = e.m.Transcript(s.ID, 100)
	if kinds(tr)[agents.EvPrompt] != 2 {
		t.Fatal("interrupted operation dispatched again")
	}
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repoTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init")
	gitTest(t, dir, "config", "user.email", "test@example.invalid")
	gitTest(t, dir, "config", "user.name", "Agent Test")
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-m", "base")
	return dir
}

func TestReviewIncludesUntrackedFilesWithoutChangingIndex(t *testing.T) {
	repo := repoTest(t)
	e := newEnv(t, withWT(agents.GitWorktrees{Root: t.TempDir()}))
	s := e.start(t, agents.StartSpec{Dir: repo, Prompt: "EDIT", NewWorktree: true})
	e.wait(t, s.ID, "done", status(agents.StatusDone))
	before := gitTest(t, s.Dir, "diff", "--cached")
	d, err := e.m.Diff(ctx10(t), s.ID)
	if err != nil || d.Error != "" {
		t.Fatalf("diff: %v %s", err, d.Error)
	}
	if d.Files != 1 || d.Additions != 2 || d.Diff.Files[0].Path != "edited.txt" {
		t.Fatalf("diff: %+v", d)
	}
	if after := gitTest(t, s.Dir, "diff", "--cached"); after != before {
		t.Fatal("review changed real index")
	}
	o := orchestrate.New(e.m, nil)
	defer o.Shutdown()
	reviewer, err := o.ReviewWith(ctx10(t), "review-once", orchestrate.ReviewParams{Target: s.ID, Kind: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.ParentID != s.ID || reviewer.PermissionMode != "plan" || !strings.Contains(reviewer.Task, "edited.txt") {
		t.Fatalf("reviewer: %+v", reviewer)
	}
	e.wait(t, reviewer.ID, "review done", status(agents.StatusDone))
	if _, err = e.m.Stop(ctx10(t), reviewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.m.Review(ctx10(t), s.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = e.m.Review(ctx10(t), s.ID, "discard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatalf("owned worktree remains: %v", err)
	}
}

func TestHandOffAndParallelWorktrees(t *testing.T) {
	repo := repoTest(t)
	e := newEnv(t, withWT(agents.GitWorktrees{Root: t.TempDir()}))
	o := orchestrate.New(e.m, nil)
	defer o.Shutdown()
	first := e.start(t, agents.StartSpec{Dir: repo, Prompt: "summarize"})
	e.wait(t, first.ID, "done", status(agents.StatusDone))
	second, err := o.HandOff(ctx10(t), "handoff-once", orchestrate.HandOffParams{From: first.ID, Kind: "claude", Note: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParentID != first.ID || second.Dir != first.Dir || !strings.Contains(second.Task, "previous-agent-summary") {
		t.Fatalf("handoff: %+v", second)
	}
	e.wait(t, second.ID, "done", status(agents.StatusDone))
	p := orchestrate.TryParams{Dir: repo, Kind: "claude", Prompt: "find a solution", N: 3}
	res, err := o.TryN(ctx10(t), "parallel-unique", p)
	if err != nil || len(res.Sessions) != 3 {
		t.Fatalf("try: %+v %v", res, err)
	}
	dirs := map[string]bool{}
	for _, s := range res.Sessions {
		dirs[s.Dir] = true
		e.wait(t, s.ID, "done", status(agents.StatusDone))
	}
	if len(dirs) != 3 {
		t.Fatal("attempts share worktrees")
	}
	again, err := o.TryN(ctx10(t), "parallel-unique", p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Sessions {
		if res.Sessions[i].ID != again.Sessions[i].ID {
			t.Fatal("try dispatched twice")
		}
	}
}

func TestLoopUntilAndCancellation(t *testing.T) {
	e := newEnv(t)
	o := orchestrate.New(e.m, nil)
	defer o.Shutdown()
	s := e.start(t, agents.StartSpec{Prompt: "begin"})
	e.wait(t, s.ID, "done", status(agents.StatusDone))
	run, err := o.LoopUntil(ctx10(t), "loop-success", orchestrate.LoopParams{Session: s.ID, Command: checkFileCommand(), MaxRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, o, run.ID, agents.RunSucceeded)
	cur, _ := e.m.Get(s.ID)
	if cur.CheckState != agents.CheckPassing {
		t.Fatalf("check = %s", cur.CheckState)
	}
	run, err = o.LoopUntil(ctx10(t), "loop-fail", orchestrate.LoopParams{Session: s.ID, Command: "exit 1", MaxRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, o, run.ID, agents.RunFailed)
	run, err = o.LoopUntil(ctx10(t), "loop-cancel", orchestrate.LoopParams{Session: s.ID, Command: longCheckCommand(), MaxRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err = o.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRun(t, o, run.ID, agents.RunCancelled)
}

func waitRun(t *testing.T, o *orchestrate.Orchestrator, id, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		runs, err := o.Runs()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.ID == id && r.Status != agents.RunRunning {
				if r.Status != want {
					t.Fatalf("run: %+v", r)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("run did not finish")
		case <-tick.C:
		}
	}
}
