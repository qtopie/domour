# Domour Project Instructions (GEMINI.md)

## Project Overview
Domour is a modular, bio-inspired AI agent framework designed for high-performance orchestration across Cloud, Edge, and Physical environments.

## Core Architecture (Biological Analogy)

We use a biological metaphor to separate cognitive reasoning from physical execution:

1.  **Brain (Cerebrum)**: The cognitive and inference center. Responsible for high-level semantic understanding, task planning (Planning), and self-correction (Reflection). It is the "System 2" slow thinker.
2.  **Cerebellum**: The logic orchestrator. It receives plans from the Cerebrum and manages tactical execution, such as the ReAct (Think-Act-Observe) loop and tool-calling sequences.
3.  **Brainstem (Motor)**: The foundation and physical layer. It handles "Survival and Instinct," including safety interception (Veto), concrete system calls (Shell, File I/O), and rendering. The Brainstem has final authority over execution.
4.  **Diencephalon**: The sensory relay and LLM adapter layer. It acts as a unified gateway to various LLM providers (DeepSeek, Gemini, OpenAI, CLI-based agents).

## System Operating Modes

Domour defines its state based on the balance between "Cognitive Power (LLM)" and "Bionic/Body Energy (I/O)":

| Mode | Cognitive (LLM) | Bionic (I/O) | Focus |
| :--- | :--- | :--- | :--- |
| **Hibernate** | ❌ Off | ❌ Off | Zero energy consumption, persistence. |
| **Vigilant** | 💤 Low (Suspended) | ⚡ High (eBPF/Sensor) | Edge perception and reflex arcs. |
| **Casual** | ⬇️ Low | ⬇️ Low | Heartbeat and basic responsiveness. |
| **Survival** | 🔄 Local-Only | ➡️ Normal | Offline autonomy for edge/remote scenarios. |
| **Balanced** | ➡️ Normal | ➡️ Normal | Optimal balance for daily interaction. |
| **Performance** | ⚡ High (Parallel) | ⚡ High (io_uring) | Maximum throughput and minimum latency. |
| **Deep Think** | ⚡ High (Reasoning) | ❌ Still | Long-chain reasoning and self-evolution. |
| **Stealth** | ➡️ Normal (Encrypted) | 🔒 Encrypted I/O | Privacy, compliance, and zero-telemetry. |
| **Diagnostic** | ➡️ Normal | 🧪 Sandbox/Mock | Tracing, debugging, and risk-free testing. |

## Development Standards
- **Language**: All code (identifiers, comments) and commit messages MUST be in **English**.
- **Observability**: Use `slog` for logging and `otel.Tracer` for tracing significant operations.
- **Safety**: The Brainstem MUST veto any unsafe commands proposed by the Brain.
- **Modularity**: Maintain strict separation between the cognitive (Brain) and physical (Motor) layers.
- **Directory Layout**: Strictly follow the structure defined in `docs/project-layout.md`. Do NOT create or use a `pkg/` directory; all public SDK components must be placed under the `ark/` directory.

## Development Guide

Three Steps Must Follow:

Design First(Documentation), Implement Next, Evaluate and Review Finnaly


## Documentation Index
- `docs/brain.md`: Detailed Brain/Thinking layer design.
- `docs/architecture/target.md`: Evolution strategy.
- `docs/features/plugins/skill-schema.md`: Definition for custom Agent skills.
- `docs/ai-agent-acp.md`: Introduction to AI Agent ACP Protocol and IDE collaboration.
