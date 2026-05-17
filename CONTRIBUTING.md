# Contributing

Thanks for showing up. fiat-lux aims to stay small, dependency-light,
and readable end-to-end. The guidelines below exist to keep it that
way.

## Local setup

Requires Go 1.26+ and [`just`](https://github.com/casey/just).

```bash
git clone https://github.com/danielriddell21/fiat-lux
cd fiat-lux
just build
```

## Common tasks

```bash
just            # list recipes
just build      # static binary -> ./fiatlux
just test       # race-enabled unit tests
just cover      # coverage report
just lint       # go vet + golangci-lint
just check      # lint + test + build, in that order
just tidy       # go mod tidy
just clean      # remove build artefacts
```

`just check` is the local gate before pushing.

## Style

- Idiomatic Go; standard project layout.
- Every exported symbol has a doc comment.
- Default to writing no comments. When a comment exists, it explains
  *why*, not *what*.
- No `panic` outside `main` and unrecoverable startup paths; return
  errors.
- No `interface{}` / `any` without a `// reason:` comment justifying
  it.
- No global mutable state.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `chore:`, `test:`, `docs:`, `refactor:`).
- Coverage should not drop noticeably between PRs.

## Testing

Brain providers are tested with stubbed `http.RoundTripper`s, not
`httptest.NewServer` (CI sandboxes sometimes block loopback). Recorded
request bodies are asserted against to lock in headers, the
`cache_control` placement, and key redaction.

Never bake API keys into tests. The `redact` helper in
`internal/brain/openai` and `internal/brain/anthropic` exists to
keep upstream error bodies from leaking secrets if a 4xx ever ends
up in a fixture — don't disarm it.

## Adding a brain provider

1. New package under `internal/brain/<name>`.
2. Implement `brain.Brain` (`Decide`, `Model`, `Provider`, `Close`).
3. Add a constructor that the spec-parser in `cmd/fiatlux/main.go`
   can dispatch to.
4. Tests with a `RoundTripper` stub. No real network calls.

## Adding a tool

1. New `Tool{...}` returned by a constructor in
   `internal/tools/catalog.go`.
2. JSON Schema in Anthropic tool-use shape.
3. `Apply` mutates the world; `RandomArgs` lets the stub brain pick
   it. For tools the sim must intercept (state outside the world,
   like `Speak` and `SpawnAgent`), keep the registry entry and let
   `Sim.invokeTool` dispatch.
