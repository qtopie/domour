# Project Layout

This project follows a structure designed to decouple core agent logic from external technical details (LLMs, databases, transport protocols) while supporting both a standalone server and an embeddable SDK.

## Directory Structure

```text
domour/
├── ark/                        # 【SDK】Agent Runtime Kit (Public SDK)
│   ├── ark.go                  # Unified gateway aggregating Hub and Governor
│   ├── bootstrap/              # Dependency Injection and Application Startup
│   ├── hub/                    # Resource Registration Hub (Tools/Skills/Providers)
│   ├── governor/               # Global Governance Center (State/Mode/Policy)
│   └── telemetry/              # Telemetry and Observability Configuration
│
├── cmd/                        # Application entry points
│   ├── domour/                 # The main background agent/service process (domourd)
│   └── domour-chat/            # A specific CLI or secondary entry point
│
├── internal/                   # Private application and core engine code
│   ├── brain/                  # Agent type implementations (Brain/Motor/Stem)
│   ├── reasoning/              # 🧠 Reasoning paradigms & thought models (ReAct, Planning)
│   ├── engine/                 # Core Agent Runtime execution engine
│   ├── proxy/                  # Agent Facade: Unified interface for internal/external agents
│   ├── bionic/                 # 【Bionic Components】The "anatomy" of a sentient agent
│   │   ├── memory/             # Multi-level memory (Short-term, Long-term)
│   │   ├── tool/               # Tool calling and definition
│   │   ├── skill/              # Skill (Markdown-based) parsing and execution
│   │   ├── session/            # Session lifecycle management
│   │   ├── plugin/             # Extension points and plugins
│   │   ├── runner/             # Execution sandbox and task runners
│   │   ├── context/            # Agent runtime context (Working set)
│   │   └── artifact/           # Managed outputs and state artifacts
│   │
│   ├── infra/                  # 【Infrastructure】Driven Adapters
│   │   ├── llm/                # LLM adapters (based on Eino SDK)
│   │   ├── storage/            # Persistence (SurrealDB, Postgres, etc.)
│   │   ├── cache/              # Caching layer (L1/L2)
│   │   ├── bus/                # Event Bus & Distributed comms (NATS/Dapr)
│   │   └── discovery/          # Service and Node discovery
│   │
│   ├── api/                    # 【API Layer】Driving Adapters
│   │   ├── grpc/               # Standard Agent gRPC protocol handlers
│   │   └── http/               # REST/Web API compatibility
│   │
│   └── config/                 # Configuration schemas and environment loading
│
├── util/                       # Shared utility functions and helpers
├── tests/                      # Integration and end-to-end tests
└── examples/                   # SDK usage examples and blueprints
```

## Core Design Principles

1.  **Separation of Concerns**: The `internal/bionic` layer contains the "what" (components), while `internal/reasoning` contains the "how" (thought patterns).
2.  **Infrastructure Agnostic**: All external services (DB, LLM, Bus) are hidden behind interfaces in `internal/infra`.
3.  **Bionic Metaphor**: The architecture strictly follows the biological separation of Brain (Reasoning), Motor (Bionic/Execution), and Brainstem (Infra/Bus).
4.  **Distributed First**: The `proxy` and `bus` layers ensure that an Agent can be a single local instance or a node in a global intelligent grid.
