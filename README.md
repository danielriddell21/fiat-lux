# fiat-lux

> *fiat lux* — let there be light.

[![CI](https://github.com/danielriddell21/fiat-lux/actions/workflows/ci.yaml/badge.svg)](https://github.com/danielriddell21/fiat-lux/actions/workflows/ci.yaml)
[![codecov](https://codecov.io/gh/danielriddell21/fiat-lux/graph/badge.svg)](https://codecov.io/gh/danielriddell21/fiat-lux)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=danielriddell21_fiat-lux&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=danielriddell21_fiat-lux)
[![Go 1.26](https://img.shields.io/badge/go-1.26-blue)](https://go.dev)
[![MIT License](https://img.shields.io/badge/licence-MIT-green)](LICENSE)

**fiat-lux** drops an AI model into a void and gives it the tools of a creator. There is no pre-built world, no map, no laws. There is an agent, an empty registry, and a vocabulary of creation. What the agent makes — water, cheese, a planet, another agent, the rules that connect them — is entirely emergent.

You watch the world materialise in real time in your terminal: a tree of entities and relationships beside the agent's stream-of-consciousness reasoning and tool calls. Pause it. Scrub it. Replay it.

## Commands

| Command | Description |
|---|---|
| `fiatlux run` | Launch the TUI |
| `fiatlux replay` | Replay a saved world tick by tick |
| `fiatlux step` | Run the sim headlessly for N steps and print a summary |
| `fiatlux serve` | Run the sim headlessly with the embedded web viewer attached |

## Install

### Homebrew
```bash
brew install danielriddell21/tap/fiatlux
```

### From source
```bash
git clone https://github.com/danielriddell21/fiat-lux
cd fiat-lux
just build
./fiatlux run        # stub brain, free
```

Press `space` to unpause.

## Documentation

Full documentation lives in the [fiat-lux wiki](https://github.com/danielriddell21/fiat-lux/wiki) — every supported brain, the multi-world and replay flows, the architecture, and the full keymap.

## Heritage

Cognitive architecture from Park et al.'s *Generative Agents: Interactive Simulacra of Human Behavior* (Stanford, 2023). fiat-lux discards Smallville's town, map, and fixed cast and keeps only the cognition, pointed at a void.
