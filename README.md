# auto8

auto8 automates chasing in the digital modes FT8 and FT4. It reads the decodes of WSJT-X and MSHV, finds the
stations that the operator needs, and tells the program which station to call. chase-queue, a component of the
operator's opsotter project, decides which stations are needed.

The project is at an early stage. `docs/SDLC.md` describes how requirements become code. The plans are in `docs/plans/`.

## Compile and test

```bash
go build ./...
go test ./...
```

## Contributing

Write all text in plain English with the rules in `.claude/output-styles/simple-english.md`. The full rule set is the skill in `.claude/skills/simple-english/`. To lint a Markdown file, run `python3 tools/simple-english/ste_lint.py --type descriptive FILE`. In Claude Code, the project configuration selects the output style and lints each Markdown file for you. `docs/SDLC.md` describes how a requirement becomes code.

## License

GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later). See [LICENSE](LICENSE). Third-party files are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
