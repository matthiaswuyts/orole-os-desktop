package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/git"
	"github.com/rayorole/orole-os-desktop/internal/github"
)

// Publish steps, as named in PublishError.
const (
	StepPrepare     = "prepare"
	StepCommit      = "commit"
	StepPush        = "push"
	StepPullRequest = "pull request"
)

// publishRemote is the only remote Publish pushes to. The session branch is pushed to a
// same-named branch there, never forced.
const publishRemote = "origin"

// ErrPublishNeedsConfirmation is returned when Push or PR is requested without the user's
// explicit confirmation. Nothing has been changed when it is returned.
var ErrPublishNeedsConfirmation = errors.New("pushing a branch or opening a pull request sends work off this machine and needs your explicit confirmation")

// PublishError reports the step that stopped Publish. Steps before it already took effect and
// are not rolled back (a commit stays committed); running Publish again continues from there.
type PublishError struct {
	Step string
	Err  error
}

func (e *PublishError) Error() string { return "publish: " + e.Step + ": " + e.Err.Error() }
func (e *PublishError) Unwrap() error { return e.Err }

func publishFail(step string, format string, args ...any) error {
	return &PublishError{Step: step, Err: fmt.Errorf(format, args...)}
}

// protectedBranches are never pushed by Publish, whatever the repository's default branch is.
var protectedBranches = []string{"main", "master", "develop", "development", "trunk", "dev", "prod", "production", "release", "stable"}

const (
	maxCommitMessage = 64 << 10
	maxPRTitle       = 256
	maxPRBody        = 60000
)

// publication is one Publish run.
type publication struct {
	m    *Manager
	spec PublishSpec
	s    Session

	branch string // checked-out branch
	remote string // origin URL as configured (credentials stripped); "" unless pushing
	repo   string // owner/name of the GitHub origin; "" unless it is one
	gh     *github.Client
	pr     *github.PullRequest
	prNote string
}

// Publish turns a stopped or idle session's work into a commit, a pushed branch and
// optionally a pull request, in that order, using git and the daemon's GitHub client.
//
//   - commit: `git add --all` + commit in the session directory with the user's configured git
//     identity. Skipped when the tree is clean. Repository hooks never run (see internal/git).
//   - push: the checked-out branch to origin with upstream set, without force. Default and
//     protected branches (main, master, ...) are refused, as is a detached HEAD.
//   - pull request: opened against spec.Base, else the branch the session started from, else the
//     repository default branch. If the session already has one, or GitHub already has one for
//     the branch, it is reused, so repeated calls only push new commits.
//
// Pushing and opening a pull request need spec.Confirmed and pass the same GitHub gates
// (policy plus confirmation, audited) as every other GitHub write; both gates are resolved before
// the first change so a denial leaves nothing half-done. The first failing step stops the run and
// is named in the returned *PublishError. The returned Session reflects the progress made.
func (m *Manager) Publish(ctx context.Context, id string, spec PublishSpec) (Session, error) {
	if spec.PR {
		spec.Push = true
	}
	spec.Message = strings.TrimSpace(spec.Message)
	spec.Title = strings.TrimSpace(spec.Title)
	spec.Base = strings.TrimSpace(spec.Base)
	if len(spec.Message) > maxCommitMessage || strings.ContainsRune(spec.Message, 0) {
		return Session{}, publishFail(StepPrepare, "the commit message must be at most %d KiB and contain no NUL", maxCommitMessage>>10)
	}
	if spec.Base != "" && (!git.ValidRef(spec.Base) || spec.Base == "HEAD") {
		return Session{}, publishFail(StepPrepare, "invalid base branch %q", spec.Base)
	}
	if spec.Push && !spec.Confirmed {
		return Session{}, &PublishError{Step: StepPrepare, Err: ErrPublishNeedsConfirmation}
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	s, err := m.Get(id)
	if err != nil {
		return Session{}, err
	}

	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.worktreeMu.RLock()
	defer m.worktreeMu.RUnlock()
	// Re-read under the locks: a concurrent Publish may have recorded the pull request.
	if s, err = m.Get(id); err != nil {
		return Session{}, err
	}
	p := &publication{m: m, spec: spec, s: s}
	if spec.Confirmed {
		ctx = github.WithConfirmation(ctx)
	}
	runErr := p.run(ctx)
	out, mutErr := m.mutate(id, func(s *Session) {
		if s.Branch == "" && p.branch != "" {
			s.Branch = p.branch
		}
		if p.pr != nil {
			s.PRURL, s.PRNumber = p.pr.HTMLURL, p.pr.Number
		}
	})
	if runErr != nil {
		return out, runErr
	}
	return out, mutErr
}

func (p *publication) run(ctx context.Context) error {
	if err := p.prepare(ctx); err != nil {
		return p.failed(StepPrepare, err)
	}
	if err := p.commit(ctx); err != nil {
		return p.failed(StepCommit, err)
	}
	if !p.spec.Push {
		return nil
	}
	if err := p.push(ctx); err != nil {
		return p.failed(StepPush, err)
	}
	if !p.spec.PR {
		return nil
	}
	if err := p.pullRequest(ctx); err != nil {
		return p.failed(StepPullRequest, err)
	}
	return nil
}

func (p *publication) failed(step string, err error) error {
	var pe *PublishError
	if errors.As(err, &pe) {
		step, err = pe.Step, pe.Err
	}
	p.m.cfg.Audit("agent.publish", p.s.ID, "error", step+": "+truncate(err.Error(), 300))
	p.m.record(p.s.ID, Event{Kind: EvSystem, Text: fmt.Sprintf("Publish stopped at %s: %s", step, truncate(err.Error(), 500))})
	return &PublishError{Step: step, Err: err}
}

func (p *publication) note(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	p.m.cfg.Audit("agent.publish", p.s.ID, "allowed", truncate(text, 300))
	p.m.record(p.s.ID, Event{Kind: EvSystem, Text: text})
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (p *publication) githubClient() (*github.Client, error) {
	if p.m.cfg.GitHub == nil {
		return nil, errors.New("GitHub is not available")
	}
	return p.m.cfg.GitHub()
}

// prepare validates everything that can be known without side effects and resolves the
// authorization gates, so a refusal or denial never leaves a commit behind without the push.
func (p *publication) prepare(ctx context.Context) error {
	s := p.s
	if !localMachine(s.Machine) {
		return errRemoteOperation
	}
	if s.Live && (s.Status == StatusWorking || s.Status == StatusNeedsYou) {
		return errors.New("the agent is still working; wait for it to finish its turn or stop it first")
	}
	if s.Dir == "" {
		return errors.New("the session has no working directory")
	}
	if fi, err := os.Stat(s.Dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("working directory %s does not exist (was the worktree discarded?)", s.Dir)
	}
	branch, err := git.CurrentBranch(ctx, s.Dir)
	switch {
	case errors.Is(err, git.ErrDetachedHead):
		return errors.New("the working directory is on a detached HEAD, not a branch; check out a branch first")
	case err != nil:
		return err
	}
	if s.Branch != "" && s.Branch != branch {
		return fmt.Errorf("the session started on branch %q but the working directory is on %q; switch back before publishing", s.Branch, branch)
	}
	p.branch = branch
	if !p.spec.Push {
		return nil
	}

	if err := p.refuseProtected(ctx, branch); err != nil {
		return err
	}
	remote, err := git.ConfiguredRemoteURL(ctx, s.Dir, publishRemote)
	if err != nil {
		return err
	}
	if remote == "" {
		return fmt.Errorf("the repository has no remote named %q to push to; add one with `git remote add %s <url>`", publishRemote, publishRemote)
	}
	p.remote = remote
	p.repo = github.RepoFromRemote(remote)
	if p.spec.PR {
		if p.repo == "" {
			return fmt.Errorf("%s is not a GitHub repository (https://github.com/<owner>/<repo> or git@github.com:<owner>/<repo>); pull requests need a github.com origin", remote)
		}
		if p.gh, err = p.githubClient(); err != nil {
			return err
		}
	} else if p.repo != "" {
		p.gh, _ = p.githubClient() // optional: only to apply the GitHub push gate when signed in
	}

	// Resolve every gate before the first change. A pull request is only gated when one has to be created.
	if p.gh != nil && p.repo != "" {
		if p.gh.Authorize == nil {
			return errors.New("GitHub authorization is unavailable")
		}
		if err := p.gh.Authorize(ctx, "github.branch.push", p.repo); err != nil {
			return err
		}
		if p.spec.PR && !p.hasPersistedPR() {
			if err := p.gh.Authorize(ctx, "github.pull.create", p.repo); err != nil {
				return err
			}
		}
		return nil
	}
	return p.m.authorize(ctx, "agent.publish.push", remote, fmt.Sprintf("Push branch %s of %s to %s.", branch, s.Dir, remote))
}

func (p *publication) refuseProtected(ctx context.Context, branch string) error {
	if slices.Contains(protectedBranches, strings.ToLower(branch)) {
		return fmt.Errorf("refusing to push the protected branch %q; publish work from its own branch (start the task in a new worktree)", branch)
	}
	if def := git.DefaultRemoteBranch(ctx, p.s.Dir, publishRemote); def != "" && def == branch {
		return fmt.Errorf("refusing to push %q, the repository's default branch; publish work from its own branch", branch)
	}
	return git.ValidBranchName(ctx, branch)
}

func (p *publication) hasPersistedPR() bool {
	return p.s.PRNumber > 0 && p.s.PRURL != "" && p.repo != "" &&
		strings.HasPrefix(strings.ToLower(p.s.PRURL), "https://github.com/"+p.repo+"/pull/")
}

func (p *publication) commit(ctx context.Context) error {
	msg := p.spec.Message
	if msg == "" {
		msg = firstLine(p.s.Title)
		if msg == "" {
			msg = firstLine(p.s.Task)
		}
		if msg == "" {
			msg = "Changes from agent session " + short(p.s.ID)
		}
		msg = truncate(msg, 200)
	}
	sha, err := git.CommitAll(ctx, p.s.Dir, msg)
	if err != nil {
		return err
	}
	if sha == "" {
		p.note("Nothing to commit: the working tree is clean")
		return nil
	}
	p.note("Committed %s on %s: %s", short(sha), p.branch, truncate(firstLine(msg), 120))
	return nil
}

func (p *publication) push(ctx context.Context) error {
	head, err := git.ResolveCommit(ctx, p.s.Dir, "HEAD")
	if err != nil {
		return fmt.Errorf("the branch has no commits to push: %w", err)
	}
	if err := git.Push(ctx, p.s.Dir, publishRemote, p.branch); err != nil {
		return fmt.Errorf("could not push %s to %s (no force push is ever attempted; check the remote, your git credentials and branch protection): %w", p.branch, publishRemote, err)
	}
	p.note("Pushed %s to %s at %s", p.branch, publishRemote, short(head))
	return nil
}

func (p *publication) pullRequest(ctx context.Context) error {
	c := p.gh
	if p.hasPersistedPR() {
		pr, err := c.GetPull(ctx, p.repo, p.s.PRNumber)
		if err != nil {
			return fmt.Errorf("could not read pull request #%d: %w", p.s.PRNumber, err)
		}
		p.pr = &pr
		p.note("Pull request #%d already exists: %s (new commits were pushed to it)", pr.Number, pr.HTMLURL)
		return nil
	}
	base, err := p.base(ctx)
	if err != nil {
		return err
	}
	if base == p.branch {
		return fmt.Errorf("the base branch %q is the branch being published; choose another base", base)
	}
	// Recover a pull request created by an earlier run whose response was lost.
	if pr, found, err := c.FindPullByHead(ctx, p.repo, p.branch, base); err != nil {
		return fmt.Errorf("could not check for an existing pull request: %w", err)
	} else if found {
		p.pr = &pr
		p.note("Pull request #%d already exists for %s: %s", pr.Number, p.branch, pr.HTMLURL)
		return nil
	}
	title := p.spec.Title
	if title == "" {
		title = firstLine(p.s.Title)
	}
	if title == "" {
		title = firstLine(p.s.Task)
	}
	if title == "" {
		return errors.New("a pull request title is required")
	}
	pr, err := c.OpenPull(ctx, p.repo, truncate(title, maxPRTitle), p.body(), p.branch, base, p.spec.Draft)
	if err != nil {
		return fmt.Errorf("branch %s is pushed but the pull request was not opened (running publish again looks for it before creating another): %w", p.branch, err)
	}
	if pr.HTMLURL == "" || pr.Number == 0 {
		return errors.New("GitHub returned a pull request without a number or URL; running publish again will find it")
	}
	p.pr = &pr
	kind := "Opened"
	if pr.Draft {
		kind = "Opened draft"
	}
	p.note("%s pull request #%d against %s: %s", kind, pr.Number, base, pr.HTMLURL)
	return nil
}

func (p *publication) body() string {
	if p.spec.Body != "" {
		return truncate(p.spec.Body, maxPRBody)
	}
	var b strings.Builder
	b.WriteString(truncate(strings.TrimSpace(p.s.Task), 20000))
	result := strings.TrimSpace(p.s.Result)
	if result == "" {
		result = strings.TrimSpace(p.s.LastMessage)
	}
	if result != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## Result\n\n")
		b.WriteString(truncate(result, 30000))
	}
	return truncate(b.String(), maxPRBody)
}

// base picks the pull request's base branch: spec.Base, else the remote branch the session's
// base commit sits on (the default branch when several do), else the repository default branch.
func (p *publication) base(ctx context.Context) (string, error) {
	if p.spec.Base != "" {
		return p.spec.Base, nil
	}
	def := git.DefaultRemoteBranch(ctx, p.s.Dir, publishRemote)
	if p.s.BaseRef != "" {
		if at, err := git.BranchesAt(ctx, p.s.Dir, p.s.BaseRef, publishRemote); err == nil {
			at = slices.DeleteFunc(at, func(n string) bool { return n == p.branch })
			if def != "" && slices.Contains(at, def) {
				return def, nil
			}
			if len(at) > 0 {
				return at[0], nil
			}
		}
	}
	if def != "" {
		return def, nil
	}
	repo, err := p.gh.GetRepo(ctx, p.repo)
	if err != nil {
		return "", fmt.Errorf("could not determine the base branch (pass one explicitly): %w", err)
	}
	if repo.DefaultBranch == "" {
		return "", errors.New("GitHub reported no default branch; pass a base branch explicitly")
	}
	return repo.DefaultBranch, nil
}
