---
title: Requirements workflow for auto8 - Plan
type: feat
date: 2026-11-20
topic: requirements-workflow
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Requirements workflow for auto8 - Plan

## Goal Capsule

- Objective: the operator drives auto8 from written requirements. A GitHub board shows the state of every requirement at any time. The build agent builds ready requirements, and GitHub merges them without the operator.
- Product authority: this plan, then `docs/SDLC.md` for the description of the process that this plan builds. The operator rules in `CLAUDE.md` also apply.
- Scope: this plan covers only the workflow. The features of auto8 itself (decoding, assist, call) are not active scope. They enter later through this workflow.
- Open blockers: none for planning. Nobody changes the GitHub configuration of `kgbvax/auto8` for R17 before the operator agrees (Dependencies).

---

## Product Contract

### Summary

Each requirement of auto8 lives in a plan file and has one GitHub issue that shows its state on a project board. The operator marks a requirement ready, and a scheduled cloud agent builds it to a pull request. If all required jobs pass, GitHub merges the pull request by itself. GitHub holds each change to TX control (code that can make the station transmit) or to the process files until the operator approves it. Work in another project waits for the operator.

### Problem Frame

auto8 is a new project, and the operator wants to give its requirements in a formal way. Up to now, work in opsotter started as a chat request. Nothing linked an idea, its refined requirement, and the code that implemented it. To learn what was done and what was open, the operator had to read plan files and git history. Every build also needed the operator in the session.

auto8 can make the station transmit, through WSJT-X or MSHV. A gate is a condition that must be true before a pull request merges. A process that builds and merges without the operator needs gates that GitHub enforces. A statement of the agent is not enough. On the free plan, GitHub enforces required jobs only on public repositories, so auto8 moved to the public repository `kgbvax/auto8` on 2026-11-20.

### Actors

- A1. The operator: the owner. Writes and refines requirements, marks them ready, reviews held pull requests, makes changes in other projects, and deploys.
- A2. The build agent: a Claude agent in the cloud that a schedule starts. Builds ready requirements and writes the summaries.
- A3. The sync step: keeps one issue for each requirement in the plan files.
- A4. GitHub: runs the CI jobs, enforces the required jobs and the code-owner review, and does the automatic merge.
- A5. Other GitHub users: can read the public repository and open issues and comments.

### Key Decisions

- Plan files are the source of the requirement text, and issues only mirror the state. The skills of the Compound Engineering (CE) plugin read files, so `ce-ideate`, `ce-brainstorm` and `ce-plan` work unchanged. Governs R1, R5, R6. (session-settled: user-approved, chosen over issues as the requirement and over one issue per plan, because the CE skills stay unchanged and the state stays per requirement.)
- GitHub Issues with a project board show the state to the operator. Governs R3, R9. (session-settled: user-directed, chosen over a status file in the repository, a reply on request, and a generated web page.)
- Builds run to the merge without the operator. Governs R10, R17. (session-settled: user-directed, chosen over a build that stops at a pull request and over a build that the operator starts.)
- auto8 has its own public repository, so that automatic merges never touch opsotter and GitHub can enforce the required jobs on the free plan. Governs R17. (session-settled: user-directed, chosen over a directory in the private opsotter repository.)
- The integration tests are simulated and run in GitHub CI. Tests with the real programs stay manual, before a deploy. Governs R15. (session-settled: user-directed, chosen over a self-hosted runner with real WSJT-X. A self-hosted runner on a public repository can run code from pull requests of other users.)
- The writing rules are a required job. Governs R16, R17. (session-settled: user-directed, chosen over a linter step of the build agent only.)
- GitHub enforces the holds with a required review by the operator as code owner, and the agents work under their own GitHub identity. Governs R18, R19, R25, R26. (session-settled: user-directed, chosen over a hold by the label of the build agent alone. With the token of the operator, GitHub cannot tell the agent from the operator.)
- A need in another project is only reported. Governs R20. (session-settled: user-directed, chosen over a prepared branch in the other project and over a build of auto8 against a fake.)
- A change to the text of a ready requirement puts its issue back to `proposed`. Governs R7. (session-settled: user-directed, chosen over an issue that stays `ready`, because no build runs on text that the operator did not mark ready.)
- The sync approach is A: sync from the plan files, one issue for each requirement, state as labels. Governs R3 to R9. (session-settled: user-directed, chosen over parent issues with sub-issues and over a done state that only a passing test can set.)
- The operator asked for a dedicated description of the process and for the simple-english rules on all text. Governs R2, R23, R24.
- Ideas get no issue. Only a requirement in a plan file gets one, so the board shows only real requirements. Governs R5.
- One build covers the ready requirements of one plan, because a plan is the unit that `ce-plan` and `ce-work` build. Governs R10.

### Requirements

Requirements and state

- R1. The text of a requirement lives only in a plan file in `docs/plans`, under a stable ID such as `R3`.
- R2. `docs/SDLC.md` describes the whole process: roles, stages, states, gates, holds and the writing rule. It also says which parts of the process work today.
- R3. Every requirement is in exactly one state: `proposed`, `ready`, `building`, `blocked`, `needs-operator`, done, or dropped. `docs/SDLC.md` defines each state and who sets it.
- R4. Whoever sets a state label removes the old state label. If an issue carries more than one state label, the build agent does not build it, and the sync step reports it.

Sync

- R5. The sync step keeps exactly one issue for each requirement of each plan file. A new requirement gets a new issue in the state `proposed`. The sync step manages only the issues that it opened. It ignores every other issue, also one with the same title.
- R6. The issue body links to the requirement in the plan file and carries no requirement text. If an issue body differs from what the sync step wrote, the sync step does not overwrite it and reports the issue to the operator.
- R7. If the text of a `ready` requirement changes, the sync step sets its issue back to `proposed` and names the reason.
- R8. If a requirement leaves its plan file, the sync step closes its issue as not planned.
- R9. A GitHub project board groups all requirement issues by state. Each card names the plan topic and the requirement ID.

Build

- R10. A build is one run of the build agent on one plan. A schedule starts the build agent a few times a day, and at most one build runs at a time. Each build takes the plan of the oldest `ready` issue and builds all `ready` requirements of that plan.
- R11. If a build stops without a result, the next run of the build agent sets the label `blocked` on its issues and names that reason.
- R12. The build agent reads the requirement text from the plan file on `main`. It never takes instructions from issue bodies, comments, or labels that someone other than the operator set.
- R13. A pull request of the build agent does not change the text of any requirement.
- R14. Before it opens a pull request, the build agent runs `ce-code-review` and the simple-english linter. It fixes what they find.

Gates

- R15. An integration job in GitHub CI runs auto8 against three peers. The first is a simulated WSJT-X that replays recorded UDP datagrams. The second is a real MQTT broker in a container. The third is a simulated chase-queue that publishes its `/state`. The recorded datagrams hold only made-up callsigns and grids, and no host addresses.
- R16. A writing job in GitHub CI lints each Markdown file that a pull request changes with `tools/simple-english/ste_lint.py`. One hit makes the job fail.
- R17. Automatic merge means that GitHub merges a pull request by itself after all required jobs pass. The required jobs on `main` are the job `test`, the integration job and the writing job. Automatic merge stays off until the integration job and the writing job exist.

Holds

- R18. A pull request that touches TX control merges only after the operator approves it. GitHub enforces this with a required review by the operator as the code owner of the TX control code. TX control is any code that can make WSJT-X or MSHV transmit. It is also the code that decides the time at which auto8 can ask for a transmission, such as the arm and presence logic. TX control code lives only in the marked paths that the operator owns. If the build agent judges that a change touches TX control, it also sets the label `needs-operator`.
- R19. A pull request that changes a process file merges only after the operator approves it, in the same way as R18. The build agent sets the label `needs-operator`. The process files are `.github/` (with the code owner file), `.claude/`, `tools/simple-english/`, `CLAUDE.md` and `docs/SDLC.md`.
- R20. If a requirement needs a change in another project, the build agent writes no code there. It describes the change in the issue and sets the label `blocked`. Another project is every repository other than `kgbvax/auto8`, for example opsotter (with chase-queue), horstreporter and StationA. A change to a vendored copy of code from another project counts as a change in that project. So does a change to the MQTT topics and payloads that another project reads.
- R21. A build or a job can fail. If the build agent cannot fix the failure in the same build, it sets the label `blocked` on the issue and names the reason.

Close

- R22. When a requirement is done, its closed issue carries a comment with a feature summary and an implementation summary. The feature summary says what the operator can now do, in the words of the requirement. The implementation summary says what changed in the code and the tests. It links to the pull request and its commits.

Writing

- R23. All text that the process or an agent writes, in the repository and on GitHub, follows the simple-english skill. This covers requirements, plans, documents, issue comments, pull request descriptions, commit messages, code comments, and user interface text. Agents reply in the output style `simple-english`.
- R24. Every session that opens the repository in Claude Code gets the `simple-english` output style, the skill and the linter without a separate install. This includes the cloud sessions of the build agent.

Access

- R25. The build agent and the sync step act on GitHub under their own identity, for example a GitHub App. That identity has access only to `kgbvax/auto8`, and it is not a code owner.
- R26. Only a label `ready` that the operator set counts. GitHub removes a label `ready` that any other identity sets, and the issue names the reason.
- R27. No token or key goes into a file, a log, an issue or a pull request.
- R28. The Claude Code configuration in `.claude/settings.json` pins each plugin to a fixed release. The operator changes a pin by hand after a review.

### Key Flows

- F1. A requirement enters
  - **Trigger:** the operator has a direction for auto8.
  - **Actors:** A1, A3
  - **Steps:** the operator runs `ce-ideate` (optional) and `ce-brainstorm`. The plan file gets numbered requirements. The operator runs the sync step, and each requirement gets an issue in `proposed`.
  - **Covered by:** R1, R5, R6, R9
- F2. Refine and mark ready
  - **Trigger:** a requirement is `proposed`.
  - **Actors:** A1, A3
  - **Steps:** the operator changes the plan file and runs the sync step. If the operator wants a requirement built, the operator sets `ready` on its issue. A later change to its text puts it back to `proposed`.
  - **Covered by:** R3, R4, R6, R7, R8
- F3. Build and merge
  - **Trigger:** the schedule starts the build agent, and at least one issue is `ready`.
  - **Actors:** A2, A4
  - **Steps:** the build agent takes the plan of the oldest `ready` issue and sets the label `building` on the issues of its ready requirements. It plans, codes, reviews and lints, then opens a pull request. GitHub runs the required jobs and does the automatic merge. The summaries go on the closed issues.
  - **Covered by:** R10, R12, R13, R14, R15, R16, R17, R22
- F4. A build stops for the operator
  - **Trigger:** a change touches TX control or a process file, needs another project, or fails.
  - **Actors:** A2, A1
  - **Steps:** the build agent sets `needs-operator` or `blocked` with a reason. GitHub holds a pull request with TX control code or a process file. The operator approves the pull request, or makes the change in the other project and sets `ready` again.
  - **Covered by:** R11, R18, R19, R20, R21, R26

### Acceptance Examples

- AE1. **Covers R18.** Given: a ready requirement adds the Reply message to WSJT-X. Then: after the build agent opens its pull request, the issue is `needs-operator`. GitHub does not merge before the operator approves.
- AE2. **Covers R20.** Given: a ready requirement needs chase-queue to read the decodes of auto8. Then: after the build agent plans it, the issue is `blocked` with a description of the chase-queue change. No branch or pull request exists in opsotter.
- AE3. **Covers R17, R21.** Given: the integration job of a pull request fails, and the build agent cannot fix it in the same build. Then: nothing merges, and the issue is `blocked` with the name of the failed job.
- AE4. **Covers R6.** Given: someone edited an issue body on GitHub. Then: after the operator runs the sync step, that issue keeps the edited body. The report of the sync step names the issue.
- AE5. **Covers R12.** Given: another GitHub user writes a comment on a ready issue and asks for a different change. Then: the build agent builds what the plan file says and ignores the comment.
- AE6. **Covers R8.** Given: the operator removed a requirement from its plan file. Then: after the sync step runs, the issue of that requirement is closed as not planned.
- AE7. **Covers R22.** Given: a pull request merges after the build session ended. Then: each closed issue gets the two summaries and keeps no label `building`.
- AE8. **Covers R10.** Given: a build runs, and the schedule starts the build agent again. Then: the second run takes no issue and stops.
- AE9. **Covers R7.** Given: the operator changes the text of a `ready` requirement. Then: after the sync step runs, the issue is `proposed` again, and no build takes it.
- AE10. **Covers R19.** Given: a build changes `.github/workflows/ci.yml`. Then: the pull request is `needs-operator`, and GitHub does not merge it before the operator approves it.
- AE14. **Covers R18.** Given: the build agent misjudges a change in the TX control paths and does not set `needs-operator`. Then: all jobs pass, but GitHub does not merge before the operator approves.
- AE15. **Covers R26.** Given: another identity sets `ready` on an issue. Then: GitHub removes the label, and no build takes the issue.
- AE11. **Covers R4.** Given: an issue carries the labels `ready` and `blocked`. Then: no build takes it, and the report of the sync step names it.
- AE12. **Covers R11.** Given: a build stopped in the middle, and its issues still carry `building`. Then: the next run sets `blocked` on them with the reason that the last build stopped without a result.
- AE13. **Covers R16, R17.** Given: a pull request adds a Markdown sentence with a banned modal verb. Then: the writing job fails, and nothing merges.

### Success Criteria

- At any time, the operator can open the board and see the state of each requirement and the reason of each stop. The operator does not need to read git history or plan files for this.
- After automatic merge is on, no change reaches `main` of auto8 without a passing job `test`, integration job and writing job.
- GitHub blocks every change to TX control code or to a process file until the operator approves it. No agent can reach a repository other than `kgbvax/auto8`.

### Scope Boundaries

- The features of auto8 (decoding, assist, call, RTTY) are not part of this plan. They enter through this workflow.
- The process never deploys. The operator deploys by hand.
- Ideas from `ce-ideate` get no issue.
- Tests with the real WSJT-X, MSHV or radio are not part of the gates. They stay a manual step before a deploy.
- A done state that only a passing test can set (approach C of the brainstorm) is deferred. It can be added later without a change to the other requirements.
- Sub-issues for each plan (approach B of the brainstorm) are deferred.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan covers the workflow only. The breakdown below is the current understanding, not a committed roadmap.

- The first feature plan of auto8 (decoding, assist, call)
  - Depends on this workflow for its build.
  - Still to decide: the draft `docs/plans/2026-11-20-1003-feat-auto8-v1-plan.md` is not in the formal format yet. It is the input of the first `ce-brainstorm` run for auto8 features, which replaces it.
  - Depends on a chase-queue change in opsotter (R20 applies). After the sync step opens its issues, the operator sets `blocked` on them with that reason. The operator sets `ready` after the chase-queue change exists.

### Dependencies / Assumptions

- The GitHub configuration of `kgbvax/auto8` needs three changes. The first is branch protection on `main` with the three required jobs of R17. The second is a required review by a code owner. The third is permission for automatic merge. Nobody makes a change before the operator agrees to it.
- The build agent runs as a scheduled Claude cloud agent. While its usage limit is used up, builds wait.
- Only collaborators can set labels on GitHub. The operator is the only collaborator. R12 depends on this.
- The operator creates the GitHub identity of R25 and installs it on `kgbvax/auto8`. The operator also gives its key to the cloud environment of the build agent. Only the operator can do these steps.
- GitHub enforces R18 only for files in the marked paths. If the build agent puts TX control code outside them, only its own label catches it. The code-owner rule and the CI jobs of R15 and R16 depend on the free plan for public repositories.

### Outstanding Questions

Deferred to Planning

- What form does the sync step take: a project skill, a script, or a GitHub Action?
- How does a cloud session of the build agent load the compound-engineering plugin, and how does the configuration pin it (R26)? The writing style, skill and linter need no plugin (R24).
- Which paths hold the TX control code, and how does the build agent keep TX control code inside them (R18)?
- Does a GitHub ruleset accept a required code-owner review with no general approval count (R18, R19)? If not, which rule gives the same effect?
- How does GitHub remove a label `ready` from another identity (R26): a GitHub Action on label events, or another way?
- Does the project board work with the identity of R25 (R9)? If not, can a token of the operator stay on the computer of the operator only (R27)?
- How often does the schedule start the build agent?
- Which run writes the summaries of R22 after GitHub merges a pull request, and which run removes the label `building`?
- How does a run find out that another build still runs (R10)?

### Sources / Research

- `docs/SDLC.md`: the process description that R2 requires.
- `docs/plans/2026-11-20-1003-feat-auto8-v1-plan.md`: the draft feature plan.
- The simple-english plugin, version 2.1.1, is the source of the output style, the skill and the linter in the repository (`THIRD_PARTY_NOTICES.md`).
- `ce-plan` stores no progress in the plan. `ce-work` derives progress from git. The board of R9 is therefore new.
- GitHub enforces required jobs and branch protection on a private repository only on a paid plan. The opsotter repository is private and has no CI.
- The CI workflow `.github/workflows/ci.yml` has one job, `test`, with `go build`, `go vet` and `go test -race`.
