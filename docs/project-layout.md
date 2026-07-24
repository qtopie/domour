# Project Layout

This project follows a structure designed to decouple core agent logic from external technical details (LLMs, databases, transport protocols) while supporting both a standalone server and an embeddable SDK.

## Directory Structure

```text
domour/
├── ark/                        # 【SDK】Agent Runtime Kit (Public SDK Layer)
│   ├── ark.go                  # Unified gateway aggregating Hub, Governor, and Agent
│   ├── agent/                  # 🧠 Agent abstractions (Agent, LLMAgent, Runner interface & Factories)
│   ├── governor/               # Global Governance Center (State, SystemMode, Policy)
│   ├── hub/                    # Capability & Resource Registration Hub (Tools/Skills/Providers)
│   ├── session/                # Session & Memory interfaces and value objects
│   ├── tool/                   # Public Tool definitions & Registration Helpers
│   ├── skill/                  # Public Skill manifests & Registration Helpers
│   ├── telemetry/              # Telemetry, OpenTelemetry & Logging Configuration
│   └── bootstrap/              # Server startup and Dependency Injection helpers
│
├── cmd/                        # Application entry points
│   ├── domour/                 # The main background agent/service process (domourd)
│   └── domour-chat/            # CLI or secondary entry point
│
├── internal/                   # Private application & core engine code (Physical Isolation)
│   ├── engine/                 # ReAct Loop, Agent execution engine & state machine
│   ├── runner/                 # Task runners and execution sandboxes
│   ├── reasoning/              # Thought models & planning algorithms
│   ├── brain/                  # Brain state management & cognitive logic
│   ├── bionic/                 # 【Bionic Components】Internal tool/skill/memory managers
│   │   ├── memory/             # Multi-level memory store implementations
│   │   ├── tool/               # Raw tool invocations & execution registry
│   │   ├── skill/              # Markdown skill parser & resolution engine
│   │   ├── session/            # Session persistence adapters
│   │   ├── context/            # Working context pipeline & OCR interceptors
│   │   └── artifact/           # Managed outputs and state artifacts
│   │
│   ├── cognitor/               # Provider LLM proxy clients (Gemini, Claude, OpenAI, etc.)
│   ├── infra/                  # Driven Adapters (SurrealDB, Badger, Dapr, NATS, OTLP)
│   ├── api/                    # Driving Adapters (gRPC Server, ACP protocol, HTTP)
│   └── config/                 # Internal configuration schemas & loading
│
├── util/                       # Shared internal utility helpers
├── tests/                      # Integration and end-to-end tests
└── examples/                   # SDK usage examples and blueprints
```

## Core Design Principles & `ark/` vs `internal/` Boundaries

### 1. Open-Closed Principle (OCP & Extensibility)
- `ark/` defines stable, high-level **interfaces** and **value objects** open for extension (via Functional Options, custom Tool/Skill plugins, or event handlers), but closed for modification.
- `internal/` provides concrete default engines and adapters that satisfy `ark/` interfaces.

### 2. Physical Encapsulation & Zero `internal/` Type Leaks (CRITICAL)
- **Zero Leak Rule**: Exported types, interface signatures, struct fields, and constructor parameters in `ark/` **MUST NEVER** reference any type under `internal/`.
- Third-party consumers importing `github.com/qtopie/domour/ark/...` must be able to compile and use the SDK without needing any access to `internal/` packages (which Go enforces physically).
- If `ark/` needs a type or state structure (e.g. `SystemMode`, `State`, `Session`), that type **MUST** be defined inside `ark/` (or a public sub-package), and `internal/` must import or adapt to `ark/`'s public type.

### 3. Lean API Surface (Avoid Bloating `ark/`)
- Do not dump internal implementation details, raw database handlers, or low-level cache engines into `ark/`.
- Keep `ark/` focused on core domain abstractions: `agent`, `governor`, `hub`, `session`, `tool`, `skill`, `telemetry`.
- Everything else belongs in `internal/`.

### 4. Delegation Architecture (Factory Constructors)
- Public sub-packages in `ark/` expose clean `New(...)` factory functions accepting Functional Options (e.g., `agent.NewLLMAgent(ctx, name, opts...)`).
- Under the hood, factory constructors instantiate internal engines (e.g., `internal/engine.Engine`) and hold them as unexported fields in public handles, delegating execution at runtime.

