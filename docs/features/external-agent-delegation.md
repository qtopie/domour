# Delegated External Agent Execution & Interception Design

This document details the architectural design for delegating complex coding subtasks to external autonomous agents (e.g., Claude Code, Gemini CLI) while maintaining strict control, safety interception, and cognitive correction loops.

---

## 1. Architectural Philosophy (Cerebellar Governance)

When Domour delegates a task to an external agent:
1. The **Cerebrum (大脑)** initiates the delegation plan.
2. The **Cerebellum (小脑)** acts as the **Governor & Interceptor** for the external agent. It configures the runtime environment, limits the tools available to the sub-agent, intercepts its tool-calling requests, and orchestrates real-time correction.
3. The **Brainstem (脑干)** executes only the authorized system operations.

```
       ┌────────────────────────────────────────────────────────┐
       │                    大脑 (Cerebrum)                     │
       │              【高层规划与策略纠偏反馈】                │
       └─────────────────────────▲──────────────────────────────┘
                                 │
                                 ▼ (Delegate Task)
       ┌────────────────────────────────────────────────────────┐
       │                    小脑 (Cerebellum)                   │
       │              【动态沙箱与外部 Agent 监视器】           │
       │   - 限制可调用工具 (Allowed Tools Gating)              │
       │   - 拦截工具请求 (Tool Call Interception)              │
       │   - 实时校准 / 纠偏上报                                │
       └─────────────────────────▲──────────────────────────────┘
                                 │
                                 ▼ (Authorized Actions)
       ┌────────────────────────────────────────────────────────┐
       │                   脑干 (Brainstem/Motor)               │
       │              【最终物理系统执行 / Veto】               │
       └────────────────────────────────────────────────────────┘
```

---

## 2. Core Mechanisms

### A. Allowed Tools Gating (权限隔离)
The external agent is initialized in a restricted context. The Cerebellum exposes a specific subset of tools (e.g. read-only filesystem tools for code inspection, but not write tools without authorization). Domour acts as the MCP host/server for the external agent, giving us absolute control over the tool schemas exposed to it.

### B. Tool Call Interception (指令拦截)
Whenever the external agent issues a tool call (such as a write/edit command):
1. The tool call is intercepted by the Cerebellum.
2. The Cerebellum validates the arguments against project constraints, or prompts for human-in-the-loop validation if required.
3. If the tool call is rejected, the Cerebellum returns an observation back to the external agent explaining why the action was blocked, guiding it to self-correct.

### C. Cerebellar Feedback to Cerebrum (小脑反馈大脑)
If the external agent gets stuck, enters an infinite loop, or continuously triggers compilation errors:
1. The Cerebellum detects the anomaly (e.g. repeated identical tool calls or failing tests).
2. It pauses the external agent's execution.
3. It packages the execution history, logs, and failure reasons into a `CognitiveCorrection` signal and sends it back to the Cerebrum.
4. The Cerebrum analyzes the feedback and updates the macro execution plan or adjusts the delegation constraints.
