# auto8

auto8 automates chasing in the digital modes FT8 and FT4. It reads the decodes of WSJT-X and MSHV, finds the
stations that the operator needs, and tells the program which station to call. chase-queue, a component of the
operator's opsotter project, decides which stations are needed.

The project is at an early stage. The draft v1 plan is in `docs/plans/`.

## Build

```bash
go build ./...
go test ./...
```

## License

GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later). See [LICENSE](LICENSE).
