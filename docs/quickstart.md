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

## Persist and replay

```bash
# Run, saving to disk; press `s` to checkpoint.
./fiatlux run --db ./kosmos.db

# Later, replay tick by tick.
./fiatlux replay --db ./kosmos.db --world kosmos --replay-tick 80ms
```

The replay is byte-exact: world events are recorded verbatim, so
state rebuilds the same way it did originally.
