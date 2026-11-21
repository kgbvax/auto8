#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Publishes the result of one build (U14 of the requirements-workflow plan).
# The publish job of .github/workflows/build.yml runs this script from a
# checkout of main, on a runner where Claude never ran. It treats the build
# result as data: a git bundle and three files that the agent wrote.
#
# The script applies the decisions of the agent only through auto8-flow,
# which accepts only blocked and needs-operator for the picked issues. It
# pushes the commits of the agent, opens the pull request, and runs holds.
#
# Environment:
#   FLOW         path of the auto8-flow binary, built from main
#   RESULT       directory with the build result (can be empty)
#   WORK         directory for files of this script
#   PICK         output of auto8-flow pick, as JSON
#   BASE         commit of main that the build started from
#   BRANCH       name of the branch of the build
#   BUILD_RESULT result of the build job: success, failure, cancelled, skipped
#   GH_TOKEN     App token (also read by auto8-flow as AUTO8_TOKEN)
#   GITHUB_REPOSITORY, AUTO8_APP_LOGIN, GITHUB_STEP_SUMMARY
#
# The script exits with 0 after it blocked the issues for a known reason.
# It exits with another code only for an unexpected error. The workflow
# then blocks the issues in its cleanup step.

set -euo pipefail

mkdir -p "$WORK"
printf '%s\n' "$PICK" > "$WORK/pick.json"

note() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$1" >> "$GITHUB_STEP_SUMMARY"
  fi
}

# block_all <pick file> <reason> sets blocked on each requirement of the
# pick. It goes one by one, so that an issue that is already blocked does
# not stop the others.
block_all() {
  local plan ids id
  plan=$(jq -r '.pick.plan // ""' "$1")
  ids=$(jq -r '(.pick.ids // []) | join(" ")' "$1")
  if [ -z "$plan" ] || [ -z "$ids" ]; then
    return 0
  fi
  for id in $ids; do
    "$FLOW" mark -state blocked -plan "$plan" -ids "$id" -reason "$2" || true
  done
  note "Blocked $plan $ids: $2"
}

# last_line <file> gives the last line of an error file, for a reason text.
last_line() {
  tail -n 1 "$1" 2>/dev/null | sed 's/^auto8-flow: //' | cut -c1-500
}

if [ "$BUILD_RESULT" != success ]; then
  block_all "$WORK/pick.json" "The build job ended with the result $BUILD_RESULT. Set ready to build it again."
  exit 0
fi

decisions="$RESULT/decisions.json"
if [ ! -f "$decisions" ]; then
  block_all "$WORK/pick.json" "The build agent wrote no decisions file, so the build did not finish. Set ready to build it again."
  exit 0
fi

# Blocked decisions first: mark with blocked closes the open App pull
# requests of the plan, so it must run before the pull request opens.
if ! "$FLOW" apply -pick "$WORK/pick.json" -decisions "$decisions" -state blocked -out "$WORK/rest.json" 2> "$WORK/apply.err"; then
  cat "$WORK/apply.err" >&2
  block_all "$WORK/pick.json" "The workflow refused the decisions of the build agent: $(last_line "$WORK/apply.err")"
  exit 0
fi

if [ -z "$(jq -r '.pick.plan // ""' "$WORK/rest.json")" ]; then
  note "No requirement is left in the build. No pull request opens."
  exit 0
fi

bundle="$RESULT/build.bundle"
if [ ! -s "$bundle" ]; then
  block_all "$WORK/rest.json" "The build made no commit for a requirement without a decision. Set ready to build it again."
  exit 0
fi

if ! git bundle verify -q "$bundle" > /dev/null 2> "$WORK/bundle.err"; then
  cat "$WORK/bundle.err" >&2
  block_all "$WORK/rest.json" "The commits of the build are not a valid git bundle."
  exit 0
fi
git fetch -q --no-tags "$bundle" HEAD
head=$(git rev-parse FETCH_HEAD)

if [ "$head" = "$BASE" ] || ! git merge-base --is-ancestor "$BASE" "$head"; then
  block_all "$WORK/rest.json" "The commits of the build do not start from main at $BASE."
  exit 0
fi

if [ -n "$(git diff --name-only "$BASE" "$head" -- .github/workflows)" ]; then
  block_all "$WORK/rest.json" "The build changes a file in .github/workflows/. The App cannot push such a change. The operator must make it."
  exit 0
fi

if ! "$FLOW" reqcheck -base "$BASE" -head "$head" -author "$AUTO8_APP_LOGIN" -root . 2> "$WORK/reqcheck.err"; then
  cat "$WORK/reqcheck.err" >&2
  block_all "$WORK/rest.json" "The build changes the text of a requirement, and only the operator can do that: $(last_line "$WORK/reqcheck.err")"
  exit 0
fi

# The token goes only into this one command line. GitHub masks it in the log.
git -c credential.helper= -c core.hooksPath=/dev/null push -q \
  "https://x-access-token:${GH_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" \
  "$head:refs/heads/$BRANCH"

plan=$(jq -r '.pick.plan' "$WORK/rest.json")
title=""
if [ -f "$RESULT/pr-title.txt" ]; then
  title=$(head -n 1 "$RESULT/pr-title.txt" | tr -d '\r' | cut -c1-200)
fi
if [ -z "${title// /}" ]; then
  title="feat: build of $plan"
fi
"$FLOW" prbody -pick "$WORK/rest.json" -in "$RESULT/pr-body.md" > "$WORK/body.md"

if ! url=$(gh pr create --repo "$GITHUB_REPOSITORY" --base main --head "$BRANCH" \
  --title "$title" --body-file "$WORK/body.md" 2> "$WORK/pr.err"); then
  cat "$WORK/pr.err" >&2
  block_all "$WORK/rest.json" "The workflow could not open the pull request of branch $BRANCH: $(last_line "$WORK/pr.err")"
  exit 0
fi
number=${url##*/}
note "Opened pull request #$number: $url"

# The check runs against the first pick, because the decisions file also
# names the requirements that are blocked now.
"$FLOW" apply -pick "$WORK/pick.json" -decisions "$decisions" -state needs-operator -out "$WORK/after.json"
"$FLOW" holds -pr "$number" -root .
