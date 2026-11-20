// SPDX-License-Identifier: AGPL-3.0-or-later

// Command auto8-flow runs the steps of the auto8 requirements workflow:
// it keeps one GitHub issue for each requirement and moves the issues
// through their states. Every command has -dry-run, which prints the
// planned GitHub changes as JSON lines and changes nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kgbvax/auto8/internal/flow"
)

const usage = `usage: auto8-flow <command> [flags]

commands:
  sync       make the issues match the plan files in docs/plans

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
