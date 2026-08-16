# Module Spec: Automatic Provider Auto-Fallback

## 1. Overview

The LLM gateway (`CognitorClient.GetClientWithOverride`) must **automatically** select a usable provider/model when the provider explicitly requested (or configured for an entry) is **unhealthy or unavailable**. This ensures the system keeps serving chat/copilot/autopilot requests on resource-constrained or partially-configured nodes (e.g. spacemit-k1 where `llamacpp` is configured but the local llama-server is down) without requiring the user to manually reconfigure.

**Design principle:** fallback selection is **fully automatic**. No user configuration is required to enable it. Candidates come from the existing `DomourConfig` (`providers` map, `enabled` flags, `default_provider`, entry overrides), ranked by a deterministic priority order. The system only ever falls back to a provider that passes a live readiness check.

## 2. Interface / API Contract

- **Entry point:** `CognitorClient.GetClientWithOverride(ctx, entry, provider, model)` — returns a `*proxy.Client`.
- **Behavior change:** if building the client for the requested `provider` fails (e.g. `provider llamacpp is unhealthy: connection refused`) **or** the resolved client fails its live readiness check (`IsReady`), the gateway tries fallback candidates in priority order. The first candidate that builds successfully **and** passes `IsReady` is returned.
- **Priority order (highest first):**
  1. The requested `provider` (with requested `model`, or the provider's configured model).
  2. The entry's configured provider (`entries[entry].provider` / `entries[entry].model`), if different from #1.
  3. The default provider (`default_provider` / `default_model`), if different from #1/#2.
  4. Remaining providers in `providers` map whose `enabled: true` and which have either an `api_key` or `base_url` configured, in lexicographic provider-name order.
- **Never considered:** providers that are `enabled != true`, providers with neither `api_key` nor `base_url` (cannot be constructed meaningfully), and the primary provider itself repeated.
- **Errors:** if no candidate (including the primary) is usable, return the original primary error (the first error encountered). If the primary was skipped because it failed readiness but a fallback works, the call **succeeds** and returns the fallback client.

## 3. Acceptance Criteria (BDD)

### Feature: Automatic fallback when the configured provider is unhealthy

#### Scenario 1: [SPEC-AUTOFB-001] Primary provider healthy → used directly, no fallback
- **Given** `DomourConfig` has `chat` entry → provider `llamacpp` and `llamacpp` passes `IsReady`
- **When** `GetClientWithOverride(ctx, "chat", "llamacpp", "gemma-2-2b")` is called
- **Then** the returned client's `Provider()` is `llamacpp` and `Model()` is `gemma-2-2b`; no fallback log is emitted
- **Mapped Test:** `internal/engine/client_test.go:TestGetClientWithOverride_PrimaryHealthy`

#### Scenario 2: [SPEC-AUTOFB-002] Primary provider unhealthy → automatic fallback to enabled provider
- **Given** `DomourConfig` has `chat` entry → provider `llamacpp`; `llamacpp` is **unhealthy** (heartbeat failed); `deepseek` is `enabled: true` with `api_key` and `base_url`, and passes `IsReady`
- **When** `GetClientWithOverride(ctx, "chat", "llamacpp", "gemma-2-2b")` is called
- **Then** the call **succeeds**, the returned client's `Provider()` is `deepseek`, and a fallback log message is emitted identifying the switch `llamacpp → deepseek`
- **Mapped Test:** `internal/engine/client_test.go:TestGetClientWithOverride_FallbackToEnabledProvider`

#### Scenario 3: [SPEC-AUTOFB-003] Primary unhealthy, fallback candidate fails readiness too → original error returned
- **Given** `llamacpp` unhealthy; `deepseek` enabled but also fails `IsReady`
- **When** `GetClientWithOverride(ctx, "chat", "llamacpp", "gemma-2-2b")` is called
- **Then** the call returns the **original** primary error (`provider llamacpp is unhealthy: ...`), not a fallback-specific error
- **Mapped Test:** `internal/engine/client_test.go:TestGetClientWithOverride_AllFallbacksFail`

#### Scenario 4: [SPEC-AUTOFB-004] Disabled / unconfigured providers never selected as fallback
- **Given** `providers` map contains `llamacpp` (enabled, unhealthy), `openai` (enabled but no `api_key`/`base_url`), and `claude` (`enabled` unset / false)
- **When** fallback selection runs
- **Then** neither `openai` nor `claude` is selected; only `deepseek` (properly enabled+configured) is tried
- **Mapped Test:** `internal/engine/client_test.go:TestGetClientWithOverride_SkipsDisabledOrUnconfigured`

#### Scenario 5: [SPEC-AUTOFB-005] Entry provider differs from requested → entry provider tried as fallback before defaults
- **Given** request passes `provider="gemini"` but `entries["chat"].provider == "deepseek"`; `gemini` unhealthy, `deepseek` enabled & ready, `default_provider` is `llamacpp` (also healthy)
- **When** `GetClientWithOverride(ctx, "chat", "gemini", "")` is called
- **Then** `deepseek` is selected (priority #2 beats default provider #3)
- **Mapped Test:** `internal/engine/client_test.go:TestGetClientWithOverride_EntryProviderPriority`

## 4. Notes / Non-Goals

- **Non-goal:** reordering config, persisting the fallback choice, or mutating `DomourConfig`. The fallback is a per-call resolution; session stickiness (active provider) is unchanged.
- **Non-goal:** fallback across entries (e.g. `copilot` entry falling back to `chat` entry's provider). Only providers within `providers` map are considered.
- The fallback must respect a bounded overall time budget; readiness checks use short timeouts (existing `IsReady` behavior).
- All external network calls in tests must be mocked (no live API calls).
