# CLAUDE.md — auto8

auto8 automates chasing in the digital modes FT8 and FT4. It reads the decodes of WSJT-X and MSHV, finds the
stations that the operator needs, and tells the program which station to call. "Needed" means what chase-queue
decides from its goals and the log database. RTTY can follow in a later version, then mostly for contests.

auto8 belongs to the operating domain of opsotter (`~/dev/opsotter`, private, GitHub `kgbvax/opsotter`). It has
its own public repository so that automatic builds and merges never touch opsotter. The operator moved it
here on 2026-11-20. The v1 plan is a draft with open questions: `docs/plans/2026-11-20-1003-feat-auto8-v1-plan.md`.
Read it before you add code.

## Rules that matter

- The opsotter rules apply (`~/dev/opsotter/CLAUDE.md`, StationA `docs/domains.md`). The station must operate
  with auto8 stopped, and WSJT-X and MSHV must work as normal without auto8.
- auto8 never transmits by itself. It asks WSJT-X or MSHV to call a station, and only while the operator is
  present and has armed auto8. The operator can stop it at once, and it stops by itself when the presence
  heartbeat goes away.
- auto8 does not evaluate awards. chase-queue (opsotter `operating/chase-queue`) owns the award and goal logic.
  auto8 sends its decoded stations to chase-queue and reads the answer from chase-queue's `/state`. A change to
  chase-queue is made in opsotter, never from this repository.
- auto8 talks to the station only over the MQTT bus contract (StationA `docs/station-integration-model.md`). It
  talks to WSJT-X and MSHV over their UDP protocol on the PC where they run. It does not change the radio, the
  PA or the antennas.
- This repository is public. Never commit callsign logs, QSO data, host addresses, credentials or config files
  with secrets. Secrets are env-only (for example `AUTO8_MQTT_PASSWORD`), never in the TOML, logs or MQTT payloads.
- Every user interface follows opsotter `docs/DESIGN.md`. Write every UI text with the simple-english skill.
- If auto8 needs StationA `shared`, import `codeberg.org/kgbvax/stationa/shared` or vendor a copy. Never import
  opsotter code.
- CI (`.github/workflows/ci.yml`) runs `go build`, `go vet` and `go test -race` on every push and PR.
- Deploy only when the user asks, every time.

## Layout

| Path | Role |
|---|---|
| `cmd/auto8/` | The command (a stub today) |
| `docs/plans/` | Requirements and plans (CE unified plans) |

## Commands

```bash
go build ./...
go test -race ./...
```
