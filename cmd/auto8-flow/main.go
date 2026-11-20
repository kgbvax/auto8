// SPDX-License-Identifier: AGPL-3.0-or-later

// Command auto8-flow runs the steps of the auto8 requirements workflow:
// it keeps one GitHub issue for each requirement and moves the issues
// through their states. Every command has -dry-run, which prints the
// planned GitHub changes as JSON lines and changes nothing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgbvax/auto8/internal/flow"
)

const usage = `usage: auto8-flow <command> [flags]

commands:
  sync       make the issues match the plan files in docs/plans
  reap       set blocked on building issues without an open App pull request
  pick       set building on the ready issues of the next plan, print it as JSON
  mark       set blocked or needs-operator on building issues of one plan
  holds      set needs-operator if an App pull request changes owned files
  reqcheck   fail if an App pull request changes the requirements
  merge      merge an App pull request when all gates pass, or block its issues
  close      copy the summaries of a closed pull request to its issues

  AUTO8_MERGE_ENABLED=true      lets merge merge; without it, merge only blocks

environment:
  AUTO8_TOKEN or GITHUB_TOKEN   GitHub token (never printed)
  GITHUB_REPOSITORY             owner/name of the repository
  GITHUB_API_URL                default https://api.github.com
  GITHUB_GRAPHQL_URL            default https://api.github.com/graphql
  AUTO8_PROJECT_ID              node ID of the project board (optional)
  AUTO8_APP_LOGIN               default opsotter-auto8[bot]
  AUTO8_OPERATOR                default kgbvax
`

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

type common struct {
	client   *flow.Client
	app      string
	operator string
	root     string
}

func setup(fs *flag.FlagSet, args []string) (*common, error) {
	dry := fs.Bool("dry-run", false, "print the planned changes as JSON lines and change nothing")
	repo := fs.String("repo", env("GITHUB_REPOSITORY", ""), "owner/name of the repository")
	root := fs.String("root", ".", "root of the repository checkout")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *repo == "" {
		return nil, errors.New("no repository: set -repo or GITHUB_REPOSITORY")
	}
	token := env("AUTO8_TOKEN", os.Getenv("GITHUB_TOKEN"))
	if token == "" && !*dry {
		return nil, errors.New("no token: set AUTO8_TOKEN or GITHUB_TOKEN")
	}
	return &common{
		client: &flow.Client{
			BaseURL:    env("GITHUB_API_URL", "https://api.github.com"),
			GraphQLURL: env("GITHUB_GRAPHQL_URL", "https://api.github.com/graphql"),
			Token:      token,
			Repo:       *repo,
			ProjectID:  os.Getenv("AUTO8_PROJECT_ID"),
			DryRun:     *dry,
			Out:        os.Stdout,
		},
		app:      env("AUTO8_APP_LOGIN", "opsotter-auto8[bot]"),
		operator: env("AUTO8_OPERATOR", "kgbvax"),
		root:     *root,
	}, nil
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("no command")
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	switch cmd {
	case "sync":
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		plans, reports := flow.LoadPlans(os.DirFS(c.root), "docs/plans")
		_, err = flow.Sync(ctx, c.client, plans, reports, c.app, stderr)
		return err
	case "reap":
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		changed, err := flow.Reap(ctx, c.client, c.app)
		for _, n := range changed {
			fmt.Fprintf(stderr, "#%d: blocked, the build stopped without a pull request\n", n)
		}
		return err
	case "pick":
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		plans, reports := flow.LoadPlans(os.DirFS(c.root), "docs/plans")
		if len(reports) > 0 {
			return fmt.Errorf("pick stopped: %s", strings.Join(reports, "; "))
		}
		p, err := flow.PickNext(ctx, c.client, plans, c.app, c.operator)
		if err != nil {
			return err
		}
		if p == nil {
			p = &flow.Pick{}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"pick": p})
	case "mark":
		state := fs.String("state", "", "blocked or needs-operator")
		reason := fs.String("reason", "", "why the state changes; written on each issue")
		plan := fs.String("plan", "", "path of the plan file")
		ids := fs.String("ids", "", "requirement IDs, separated by commas")
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		if *plan == "" || *ids == "" {
			return errors.New("mark needs -plan and -ids")
		}
		return flow.Mark(ctx, c.client, c.app, *plan, strings.Split(*ids, ","), *state, *reason)
	case "holds":
		pr := fs.Int("pr", 0, "number of the pull request")
		owners := fs.String("codeowners", ".github/CODEOWNERS", "CODEOWNERS file of the base branch, relative to -root")
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		if *pr == 0 {
			return errors.New("holds needs -pr")
		}
		data, err := os.ReadFile(filepath.Join(c.root, *owners))
		if err != nil {
			return err
		}
		owned, err := flow.Holds(ctx, c.client, c.app, flow.ParseOwners(string(data)), *pr)
		for _, f := range owned {
			fmt.Fprintf(stderr, "owned file changed: %s\n", f)
		}
		return err
	case "reqcheck":
		base := fs.String("base", "", "git ref of the base")
		head := fs.String("head", "HEAD", "git ref of the head")
		author := fs.String("author", "", "login of the pull request author")
		root := fs.String("root", ".", "root of the repository checkout")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *base == "" || *author == "" {
			return errors.New("reqcheck needs -base and -author")
		}
		bfs, err := flow.GitFS(*root, *base, "docs/plans")
		if err != nil {
			return err
		}
		hfs, err := flow.GitFS(*root, *head, "docs/plans")
		if err != nil {
			return err
		}
		app := env("AUTO8_APP_LOGIN", "opsotter-auto8[bot]")
		if diffs := flow.ReqCheck(bfs, hfs, "docs/plans", *author, app); len(diffs) > 0 {
			fmt.Fprint(stderr, flow.FormatDiffs(diffs))
			return fmt.Errorf("the pull request changes %d requirements, and only the operator can change requirements", len(diffs))
		}
		fmt.Fprintln(stderr, "reqcheck: the requirements did not change")
		return nil
	case "merge":
		pr := fs.Int("pr", 0, "number of the pull request")
		mainRef := fs.String("main", "origin/main", "git ref of the current main")
		headRef := fs.String("head", "", "git ref of the pull request head")
		require := fs.String("require", strings.Join(flow.RequiredJobs, ","), "required jobs, separated by commas")
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		if *pr == 0 || *headRef == "" {
			return errors.New("merge needs -pr and -head")
		}
		mfs, err := flow.GitFS(c.root, *mainRef, "docs/plans")
		if err != nil {
			return err
		}
		hfs, err := flow.GitFS(c.root, *headRef, "docs/plans")
		if err != nil {
			return err
		}
		res, err := flow.Merge(ctx, c.client, flow.MergeInput{
			App: c.app, Operator: c.operator, Pull: *pr, Required: strings.Split(*require, ","),
			Main: mfs, Head: hfs, Enabled: os.Getenv("AUTO8_MERGE_ENABLED") == "true",
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "merge #%d: merged=%v, %s\n", *pr, res.Merged, res.Reason)
		return nil
	case "close":
		pr := fs.Int("pr", 0, "number of the pull request")
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		if *pr == 0 {
			return errors.New("close needs -pr")
		}
		return flow.Close(ctx, c.client, c.app, *pr)
	case "help", "-h", "--help":
		fmt.Fprint(stderr, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "auto8-flow:", strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
