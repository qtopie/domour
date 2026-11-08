# AGENTS.md - System Operating Guidelines

Welcome Agent! You are a core collaborator in this repository. You MUST strictly adhere to these operational rules.

## 1. Context Loading & Memory Rule
- **Always Check `.agents/TASK.md` First:** Before taking any action, read `.agents/TASK.md` to restore context.
- **Maintain `.agents/TASK.md`:** Update `.agents/TASK.md` checklist items as you progress. If interrupted, write the current status under `Current Context`.

## 2. Spec-First Gate (Strict Enforcement)
- **SSOT (Single Source of Truth):** All behavioral contracts belong in `specs/`. Never implement feature logic without an approved Spec.
- **No Spec, No Code:** 
  1. Draft/Update files in `specs/` or `docs/rfcs/`.
  2. Wait for explicit user approval (`APPROVE`).
  3. Generate or update test harness fixtures (`harness/`) and test stubs (`testings/`).
  4. Only then implement business logic code.

## 3. Harness Engineering & Testing Gate
- **Harness-Driven Development:** Maintain fixtures, mocks, and runners in `harness/`.
- **Structured Diagnostic Feedback:** When tests fail, read the `Harness Failure Report` automatically written to `.agents/TASK.md` to perform targeted fixes instead of guessing logs.
- **No Shallow Tests:** Never write trivial Getter/Setter tests; tests must cover real boundary conditions and error scenarios.
- **Mock External Dependencies:** Always mock databases, network I/O, external RPCs, and hardware APIs in unit tests and harness mocks (`harness/mocks/`).

## 4. Grounding & Code Rules
- **Read Before Write:** Read target files and their dependencies before editing.
- **Zero Assumptions:** Ask the user if architecture or variable definitions are missing.
- **Minimal Diff:** Modify only what is required. Do not refactor unrelated code.

## 5. Execution & Safety Red Lines
- **Prohibited Commands:** Never run `git push --force`, `rm -rf /`, or alter external systems.
- **Mandatory Self-Validation:** Run `./scripts/check.sh` (or `./scripts/check-harness.sh`) before marking a task complete.
- **Error Limit:** If test/compile fixes fail > 3 times, stop and ask the user for guidance.

## 6. Unified OpenTelemetry & Zero-Impact Observability Rule (统一可观测性与零影响规范)
- **W3C Standard Only:** 所有 gRPC / HTTP / EventBus / Workflow 链路追踪必须 100% 遵循 W3C `TraceContext` (`traceparent`) 标准传播，严禁使用私有 `x-trace-id` metadata。
- **Host-Aware Embedded Mode:** 当 `domour` 作为库被宿主（`cosmos-star` / `cosmos-assistant`）内嵌调用时，`ark/telemetry.Setup` 必须检测并复用宿主已注册的全局 `TracerProvider` / `MeterProvider` / `slog` Logger，严禁覆盖宿主的全局实例。
- **Unified Dual-Track Logging (`slog` Only):** 全面使用 `log/slog`（优先调用 `slog.InfoContext(ctx, ...)`），禁止直接向 `os.Stderr` 手拼日志文本；统一遵循「终端 Text + 文件 JSON Lines + 自动注入 `service`/`component`/`trace_id`/`span_id`」规范。
- **100% Non-Blocking & Fail-Safe:** 任何 OTel Exporter 初始化、采样与 Shutdown 必须非阻塞并具备超时保护，可观测性组件异常或关闭时对核心推理与工具执行必须 100% 零影响。
