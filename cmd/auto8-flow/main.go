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

	"github.com/opsotter/auto8/internal/flow"
)

const usage = `usage: auto8-flow <command> [flags]

commands:
  sync       make the issues match the plan files in docs/plans
  reap       set blocked on building issues without an open App pull request
  pick       set building on the ready issues of the next plan, print it as JSON
  mark       set blocked or needs-operator on building issues of one plan
  apply      apply the state decisions of the build agent for the picked issues
  prbody     build the pull request body of a build from the text of the agent
  holds      set needs-operator if an App pull request changes owned files
  reqcheck   fail if an App pull request changes the requirements
  merge      merge an App pull request when all gates pass, or block its issues
  close      copy the summaries of a closed pull request to its issues

  AUTO8_MERGE_ENABLED=true      lets merge merge. Without it, merge only blocks.

environment:
  AUTO8_TOKEN or GITHUB_TOKEN   GitHub token (never printed)
  GITHUB_REPOSITORY             owner/name of the repository
  GITHUB_API_URL                default https://api.github.com
  GITHUB_GRAPHQL_URL            default https://api.github.com/graphql
  AUTO8_PROJECT_ID              node ID of the project board (optional)
  AUTO8_APP_LOGIN               default opsotter-auto8[bot]
  AUTO8_OPERATOR                default kgbvax
`

const (
	defaultApp      = "opsotter-auto8[bot]"
	defaultOperator = "kgbvax"
)

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
		app:      env("AUTO8_APP_LOGIN", defaultApp),
		operator: env("AUTO8_OPERATOR", defaultOperator),
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
		plans, reports := flow.LoadPlans(os.DirFS(c.root), flow.PlansDir)
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
		plans, reports := flow.LoadPlans(os.DirFS(c.root), flow.PlansDir)
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
	case "apply":
		pickFile := fs.String("pick", "", "JSON file with the output of pick")
		decFile := fs.String("decisions", "", "JSON file with the decisions of the build agent")
		state := fs.String("state", "", "apply only the decisions with this state: blocked or needs-operator")
		out := fs.String("out", "", "file for the pick without the blocked requirements")
		c, err := setup(fs, rest)
		if err != nil {
			return err
		}
		if *pickFile == "" || *decFile == "" || *out == "" {
			return errors.New("apply needs -pick, -decisions and -out")
		}
		p, err := readPick(*pickFile)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(*decFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		ds, err := flow.ParseDecisions(data)
		if err != nil {
			return err
		}
		restPick, err := flow.ApplyDecisions(ctx, c.client, c.app, p, ds, *state)
		if err != nil {
			return err
		}
		return writePick(*out, restPick)
	case "prbody":
		pickFile := fs.String("pick", "", "JSON file with the output of pick")
		in := fs.String("in", "", "Markdown file with the text of the agent; a missing file gives an empty text")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *pickFile == "" || *in == "" {
			return errors.New("prbody needs -pick and -in")
		}
		p, err := readPick(*pickFile)
		if err != nil {
			return err
		}
		text, err := os.ReadFile(*in)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_, err = io.WriteString(os.Stdout, flow.PullBody(p, string(text)))
		return err
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
		bfs, err := flow.GitFS(*root, *base, flow.PlansDir)
		if err != nil {
			return err
		}
		hfs, err := flow.GitFS(*root, *head, flow.PlansDir)
		if err != nil {
			return err
		}
		app := env("AUTO8_APP_LOGIN", defaultApp)
		if diffs := flow.ReqCheck(bfs, hfs, flow.PlansDir, *author, app); len(diffs) > 0 {
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
		mfs, err := flow.GitFS(c.root, *mainRef, flow.PlansDir)
		if err != nil {
			return err
		}
		hfs, err := flow.GitFS(c.root, *headRef, flow.PlansDir)
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
		return flow.Close(ctx, c.client, c.app, c.operator, *pr)
	case "help", "-h", "--help":
		fmt.Fprint(stderr, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// readPick reads a file in the format that pick prints: {"pick": {...}}.
func readPick(path string) (flow.Pick, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return flow.Pick{}, err
	}
	var w struct {
		Pick *flow.Pick `json:"pick"`
	}
	if err := json.Unmarshal(data, &w); err != nil || w.Pick == nil {
		return flow.Pick{}, fmt.Errorf("%s is not the output of pick", path)
	}
	if w.Pick.Plan == "" || len(w.Pick.IDs) == 0 {
		return flow.Pick{}, fmt.Errorf("%s names no plan and no requirements", path)
	}
	return *w.Pick, nil
}

// writePick writes p in the format that pick prints. A pick without
// requirements gets an empty plan, as pick prints it when it finds nothing.
func writePick(path string, p flow.Pick) error {
	if len(p.IDs) == 0 {
		p = flow.Pick{}
	}
	data, err := json.Marshal(map[string]any{"pick": p})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "auto8-flow:", strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
