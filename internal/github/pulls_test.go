//go:build unix

package github

import (
	"os"
	"strings"
	"testing"
)

// TestAChecksStateIsGitHubs: check runs and commit statuses are read as
// GitHub's list of checks reads them — queued and running pending, success
// and neutral passing, skipped apart, every other conclusion failing. If it
// regresses, a pull request whose checks are still running shows as failed,
// or one that failed as passing.
func TestAChecksStateIsGitHubs(t *testing.T) {
	for c, want := range map[wireCheck]string{
		{Type: "CheckRun", Status: "QUEUED"}:                             CheckPending,
		{Type: "CheckRun", Status: "IN_PROGRESS"}:                        CheckPending,
		{Type: "CheckRun", Status: "COMPLETED", Conclusion: "SUCCESS"}:   CheckPass,
		{Type: "CheckRun", Status: "COMPLETED", Conclusion: "NEUTRAL"}:   CheckPass,
		{Type: "CheckRun", Status: "COMPLETED", Conclusion: "SKIPPED"}:   CheckSkipped,
		{Type: "CheckRun", Status: "COMPLETED", Conclusion: "FAILURE"}:   CheckFail,
		{Type: "CheckRun", Status: "COMPLETED", Conclusion: "TIMED_OUT"}: CheckFail,
		{Type: "StatusContext", State: "PENDING"}:                        CheckPending,
		{Type: "StatusContext", State: "SUCCESS"}:                        CheckPass,
		{Type: "StatusContext", State: "ERROR"}:                          CheckFail,
	} {
		if got := c.check().State; got != want {
			t.Errorf("%+v = %s, want %s", c, got, want)
		}
	}
}

// TestPullRequestsAreListedAndActedOn: the list asks gh for the preset with
// what was typed, without the checks, which a second call reads and sums —
// together, on a busy repository, GitHub gave up with a 504; merge, close, reopen and
// ready are gh's own commands, a merge by the method asked. If it
// regresses, "needs my review" lists everybody's, or a squash merge makes a
// merge commit.
func TestPullRequestsAreListedAndActedOn(t *testing.T) {
	repo := Repo{Owner: "o", Name: "r"}
	args := fakeGh(t, `[{"number":9,"title":"Fix it","state":"OPEN","isDraft":true,"author":{"login":"ana"},"labels":[],
		"headRefName":"issue-4-fix","baseRefName":"main","updatedAt":"2026-09-20T10:00:00Z","url":"u","reviewDecision":"APPROVED",
		"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
		{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"},{"__typename":"StatusContext","state":"PENDING"}]}]`, "", 0)
	prs, err := ListPRs(repo, PRFilterReview, "label:ui")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || !prs[0].Draft || prs[0].State != "open" || prs[0].Head != "issue-4-fix" {
		t.Errorf("prs: %+v", prs)
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "--search\nlabel:ui is:open review-requested:@me sort:updated-desc\n") || strings.Contains(string(got), "statusCheckRollup") {
		t.Errorf("list: %q", got)
	}
	// The checks come apart, for the same search, counted.
	checks, err := PRChecks(repo, PRFilterReview, "label:ui")
	if err != nil {
		t.Fatal(err)
	}
	if checks[9] != (Checks{Pass: 1, Fail: 1, Pending: 1}) {
		t.Errorf("checks: %+v", checks)
	}
	if got, _ := os.ReadFile(args); !strings.Contains(string(got), "number,statusCheckRollup") || !strings.Contains(string(got), "--limit\n30\n") {
		t.Errorf("checks: %q", got)
	}
	for _, c := range []struct {
		do   func() error
		want string
	}{
		{func() error { return MergePR(repo, 9, MergeSquash) }, "pr\nmerge\n9\n--repo\no/r\n--squash\n"},
		{func() error { return ClosePR(repo, 9) }, "pr\nclose\n9\n--repo\no/r\n"},
		{func() error { return ReopenPR(repo, 9) }, "pr\nreopen\n9\n--repo\no/r\n"},
		{func() error { return ReadyPR(repo, 9) }, "pr\nready\n9\n--repo\no/r\n"},
	} {
		if err := c.do(); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(args); string(got) != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
	if err := MergePR(repo, 9, "--admin"); err == nil {
		t.Error("a merge method gh does not have was passed on")
	}
}
