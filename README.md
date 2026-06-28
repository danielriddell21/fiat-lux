# fiat-lux

> *Let there be light.*

[![CI](https://github.com/danielriddell21/fiat-lux/actions/workflows/ci.yaml/badge.svg)](https://github.com/danielriddell21/fiat-lux/actions/workflows/ci.yaml)
[![codecov](https://codecov.io/gh/danielriddell21/fiat-lux/graph/badge.svg)](https://codecov.io/gh/danielriddell21/fiat-lux)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=danielriddell21_fiat-lux&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=danielriddell21_fiat-lux)
[![Go 1.26](https://img.shields.io/badge/go-1.26-blue)](https://go.dev)
[![MIT License](https://img.shields.io/badge/licence-MIT-green)](LICENSE)

**fiat-lux** drops an AI model into a void and gives it the tools of a
creator. There is no pre-built world, no map, no laws. There is an
agent, an empty registry, and a vocabulary of creation. What the agent
makes — water, cheese, a planet, another agent, the rules that connect
them — is entirely emergent.

You watch the world materialise in real time in your terminal: a tree
of entities and relationships beside the agent's stream-of-consciousness
reasoning and tool calls. Pause it. Scrub it. Replay it.

## Highlights

- **Five brain providers**: Anthropic, OpenAI, Ollama, any
  OpenAI-compatible endpoint, and a free stub for tests.
- **Smallville-style memory**: recency × importance × relevance
  retrieval with periodic reflection.
- **Multi-agent**: `SpawnAgent` lets a creator spin up new agents with
  their own brain config; `Speak` broadcasts between them.
- **Multi-world**: a YAML config runs N independent worlds in
  parallel; `ctrl-tab` swaps focus.
- **Replay**: every mutation is event-sourced, so a saved world
  rebuilds byte-exactly.
- **Web viewer**: optional embedded HTTP server renders the kosmos as
  a live force-directed graph. Add `--web-addr 127.0.0.1:8080`.

## Quick start
```bash
git clone https://github.com/danielriddell21/fiat-lux
cd fiat-lux
just build
./fiatlux run        # stub brain, free
```

Press `space` to unpause. See [docs/quickstart.md](./docs/quickstart.md)
for Ollama, Anthropic, OpenAI, multi-world, and replay walkthroughs.

## Install

### Homebrew
```bash
brew install danielriddell21/tap/fiatlux
```

## Documentation
- [Quickstart](./docs/quickstart.md) — every supported brain and the
  multi-world / replay flows
- [Architecture](./docs/architecture.md) — diagrams, the per-tick
  loop, memory retrieval, the brain interface
- [Key bindings](./docs/keybindings.md) — full keymap
- [Contributing](./CONTRIBUTING.md) — local setup, testing, and the
  bar for new brains and tools

## Heritage

Cognitive architecture from Park et al.'s *Generative Agents:
Interactive Simulacra of Human Behavior* (Stanford, 2023). fiat-lux
discards Smallville's town, map, and fixed cast and keeps only the
cognition, pointed at a void.
