package server

import (
	"fmt"
	"strings"

	"github.com/sousaakira/tend/internal/github"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
)

// GitHubIssues is github.issues: the issues of the repository the project a
// pane is working in is on — the directory a new tab from it would start in
// (followDir) — or, with no pane, the server's own directory.
func (s *Server) GitHubIssues(p proto.GitHubIssuesParams) (proto.GitHubIssuesResult, error) {
	spec := PaneSpec{}
	s.followDir(&spec, session.PaneID(p.Pane))
	dir := spec.Dir
	if dir == "" {
		dir = s.cfg.Dir
	}
	out := proto.GitHubIssuesResult{Dir: dir, Issues: []proto.GitHubIssue{}}
	repos, err := github.ReposFor(dir)
	if err != nil {
		return out, err
	}
	repo := repos[0]
	for _, r := range repos {
		out.Remotes = append(out.Remotes, r.Remote)
		if r.Remote == p.Remote {
			repo = r
		}
	}
	out.Repo, out.Remote = repo.Slug(), repo.Remote
	filter := github.FilterOpen
	if p.Filter >= 0 && p.Filter < len(github.Filters) {
		filter = github.Filters[p.Filter]
	}
	issues, total, err := github.List(repo, filter, p.Query)
	if err != nil {
		return out, err
	}
	out.Total = total
	for _, i := range issues {
		out.Issues = append(out.Issues, wireIssue(i))
	}
	return out, nil
}

// repoOf reads owner/name, and with number an issue's number too.
func repoOf(p proto.GitHubIssueParams, number bool) (github.Repo, error) {
	owner, name, ok := strings.Cut(p.Repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || (number && p.Number <= 0) {
		return github.Repo{}, fmt.Errorf("an issue is named by owner/name and a number, not %q #%d", p.Repo, p.Number)
	}
	return github.Repo{Owner: owner, Name: name}, nil
}

// GitHubIssueWrite is what the panel writes to GitHub: a comment, an issue
// closed or opened again, a new issue.
func (s *Server) GitHubIssueWrite(method string, p proto.GitHubIssueParams) (any, error) {
	repo, err := repoOf(p, method != proto.MethodGitHubIssueCreate)
	if err != nil {
		return nil, err
	}
	switch method {
	case proto.MethodGitHubIssueComment:
		return struct{}{}, github.AddComment(repo, p.Number, p.Body)
	case proto.MethodGitHubIssueState:
		switch p.State {
		case "closed":
			return struct{}{}, github.Close(repo, p.Number, p.Reason)
		case "open":
			return struct{}{}, github.Reopen(repo, p.Number)
		}
		return nil, fmt.Errorf("an issue is open or closed, not %q", p.State)
	}
	n, url, err := github.Create(repo, p.Title, p.Body)
	if err != nil {
		return nil, err
	}
	return proto.GitHubIssueCreated{Number: n, URL: url}, nil
}

// GitHubIssue is github.issue: one issue with its thread.
func (s *Server) GitHubIssue(p proto.GitHubIssueParams) (proto.GitHubIssueDetail, error) {
	repo, err := repoOf(p, true)
	if err != nil {
		return proto.GitHubIssueDetail{}, err
	}
	d, err := github.Get(repo, p.Number)
	if err != nil {
		return proto.GitHubIssueDetail{}, err
	}
	out := proto.GitHubIssueDetail{GitHubIssue: wireIssue(d.Issue), Body: d.Body, Created: d.Created.Unix()}
	for _, c := range d.Thread {
		out.Thread = append(out.Thread, proto.GitHubComment{Author: c.Author, Body: c.Body, Created: c.Created.Unix()})
	}
	return out, nil
}

func wireIssue(i github.Issue) proto.GitHubIssue {
	return proto.GitHubIssue{
		Number: i.Number, Title: i.Title, State: i.State, Author: i.Author,
		Labels: i.Labels, Assignees: i.Assignees, Comments: i.Comments,
		Updated: i.Updated.Unix(), URL: i.URL,
	}
}
