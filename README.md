# domour

**domour** is a lightweight, distributed bionic AI agent runtime written in Go. Designed for environments ranging from resource-constrained edge devices (e.g. Milk-V Duo S) to cloud clusters, it provides a decoupled, secure, and extensible operating environment for intelligent agents.

---

## Key Highlights

- **Bionic Architecture**: Strict separation of concerns between **Brain** (non-deterministic cognition and planning) and **Motor** (deterministic tool execution, safety guards, and validation).
- **Zero-Dependency 3-Tier SPI**: Pluggable Service Provider Interface (`runtime`, `capability`, `infra`). Runs out-of-the-box with in-memory state; seamlessly integrates with external backends (Dapr, SurrealDB) when needed.
- **Embedded llama.cpp Runtime**: Native in-tree CGo bridge for local GGUF model inference with ChatML rendering, streaming UTF-8 buffering, and logits extraction.
- **Protocol-First**: Built-in support for ACP (Agent Communication Protocol) and flexible chat interfaces.

---

## Quick Start

### Installation & Build

```bash
git clone https://github.com/qtopie/domour.git
cd domour
go build -o bin/domour ./cmd
```

### CLI Usage

```bash
# Interactive agent chat CLI (in-memory default)
./bin/domour chat

# One-shot prompt
./bin/domour chat "Explain the bionic agent architecture in 2 sentences"

# Run local GGUF model via in-tree llama.cpp
./bin/domour llamacpp chat -m /path/to/model.gguf -p "Hello!"

# Run ACP server in stdio mode
./bin/domour acp
```

---

## Architecture Overview

```
       ┌────────────────────────┐
       │   Brain (Cognition)    │ ◄── Planning, Semantic Reasoning
       └───────────┬────────────┘
                   │ Intent & Tasks
       ┌───────────▼────────────┐
       │     Motor (Control)    │ ◄── Deterministic Tool Execution & Safety
       └───────────┬────────────┘
                   │
    ┌──────────────┴──────────────┐
    │ 3-Tier SPI (ark/spi)        │
    ├─────────────┬───────────────┤
    │ Runtime     │ Router, Cognitor, Lifecycle
    │ Capability  │ ToolInvoker, SkillRegistry
    │ Infra       │ SessionStore, EventBus, Orchestrator
    └─────────────┴───────────────┘
```

---

## Development & Verification

Run the unified linting and Harness engineering test suite:

```bash
./scripts/check.sh
```

---

