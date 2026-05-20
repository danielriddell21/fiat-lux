# Quickstart

Requires Go 1.26+ and [`just`](https://github.com/casey/just).

```bash
git clone https://github.com/danielriddell21/fiat-lux
cd fiat-lux
just build
./fiatlux help
```

## Stub brain (free)

The stub picks a random valid tool every tick — useful for trying the
TUI before plugging in a real brain.

```bash
./fiatlux run
```

Press `space` to unpause.

## Local model via Ollama

```bash
ollama pull qwen3:8b
ollama serve &
./fiatlux run --brain=ollama --tick=4s
```

Or use the bundled config:

```bash
./fiatlux run --config examples/local-qwen.yaml --tick=4s
```

## Anthropic Claude

```bash
export ANTHROPIC_API_KEY=...
./fiatlux run --brain=anthropic --tick=4s
```

Prompt caching is on by default, so the stable parts of every prompt
are cached after the first call.

```bash
./fiatlux run --config examples/cloud-haiku.yaml --tick=4s
```

## OpenAI

```bash
export OPENAI_API_KEY=...
./fiatlux run --brain=openai --tick=4s
```

## OpenAI-compatible endpoints (LM Studio, vLLM, OpenRouter)

```bash
./fiatlux run --brain=openaicompat:http://localhost:1234/v1::my-model
```

## Multiple worlds in parallel

```bash
./fiatlux run --config examples/multi-world-stub.yaml --tick=2s
```

Once the TUI is up, press `ctrl-tab` to switch between worlds. Each
has its own creator, memory, and event log.

## Web viewer

Add `--web-addr` to any `run` command to serve a force-directed graph
of the kosmos at `http://localhost:8080` while the TUI is running.

```bash
./fiatlux run --brain=stub --web-addr 127.0.0.1:8080
```

The browser view shows entities as nodes and relationships as edges.
A toggle at the top right switches to a tree view that mirrors the
TUI's Creation Tree. The address is also configurable from YAML:

```yaml
web:
  addr: 127.0.0.1:8080
```

## Persist and replay

```bash
# Run, saving to disk; press `s` to checkpoint.
./fiatlux run --db ./kosmos.db

# Autosave every 30 seconds instead of pressing 's':
./fiatlux run --db ./kosmos.db --save-mode interval:30s

# Quit and re-run: the root agent picks up its previous memory.
./fiatlux run --db ./kosmos.db

# Later, replay tick by tick.
./fiatlux replay --db ./kosmos.db --world kosmos --replay-tick 80ms
```

The replay is byte-exact: world events are recorded verbatim, so
state rebuilds the same way it did originally.

When `--db` is set, the **root agent's memory** also persists across
sessions: thoughts, actions, outcomes, and reflections from prior
runs come back when you re-open the same world. Spawned-agent memory
is session-scoped for now.

## Headless mode

Run the sim without the TUI for CI smoke tests, benchmarks, or
scripted scenarios. Useful when there's no `/dev/tty` (containers,
CI runners) or when you just want stats.

```bash
# Run 50 stub-brain steps and print a JSON summary.
./fiatlux step --count 50 --brain stub --json

# Human-readable, with a saved DB and reflection.
./fiatlux step --count 200 --brain stub --db ./bench.db --reflect-interval 20
```

The summary reports tick count, entity / relationship / agent
counts, tool-call counts, and elapsed time. A non-empty `--db`
triggers a final save so the run is reloadable.

## Remote database (libSQL)

The same `--db` flag accepts libSQL URLs, so a kosmos can live on a
server instead of a local file. Schema and behaviour are identical;
the only thing that changes is the driver.

### Hosted: Turso

Sign up at [turso.tech](https://turso.tech), create a database, and
grab the URL + auth token. Then:

```bash
./fiatlux run \
  --db 'libsql://my-kosmos.turso.io?authToken=eyJhbGc...' \
  --save-mode interval:30s
```

### Self-hosted: sqld

Run the libSQL server yourself:

```bash
docker run -d --name sqld \
  -p 8080:8080 -v $PWD/sqld-data:/var/lib/sqld \
  ghcr.io/tursodatabase/libsql-server

./fiatlux run --db 'http://localhost:8080' --save-mode interval:10s
```

`sqld` is a single binary if you'd rather not use Docker — see the
[upstream README](https://github.com/tursodatabase/libsql/tree/main/libsql-server).

### Local libSQL file

If you want the libSQL client without a server (e.g. to test the
remote code path locally), pass a `libsql://` URL pointing at a
sqld instance you've started locally.
