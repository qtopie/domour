# CLI Model Provider Proxy & Vproxy Wrapping Integration Test Guide

This document describes the automated integration test design and execution instructions to verify that the `domour` CLI chat model provider correctly performs transparent proxy wrapping using `vproxy`.

---

## 🛠️ Automated Integration Test Design

To guarantee 100% hermetic and stable integration testing without requiring a live remote proxy server or administrator-level system proxy settings, we use a **harness-mocking technique**:

1. **Vproxy Execution Mock**: We dynamically generate a lightweight mock shell script named `vproxy` within a temporary directory and temporarily prepend this directory to the system `PATH`.
2. **Execution Inspection**: The mock `vproxy` script writes all command line arguments it receives and dumps the contents of the temporary `vproxy.json` configuration file into a trace file.
3. **Trigger**: We run `CLIChatModel.performHealthCheck()` with a mock model command (`true`) and a mock `ProxyURL`.
4. **Validation**: We read the generated trace file and assert that:
   - `vproxy` was actually invoked instead of the direct command.
   - The temporary `-c` config path was passed correctly.
   - The serialized `vproxy.json` configuration exists and contains the configured `upstreams` and routing rules.
5. **Auto-Cleanup**: The test automatically restores the original `PATH` and deletes the temporary script and trace files.

---

## 🚀 Execution Instructions (For AI or Developer)

To run the automated integration test, simply execute the following command in the `domour` project directory:

```bash
go test -v -run TestVProxyWrappingIntegration ./internal/core/llm/cli/...
```

---

## 💡 Manual Verification Protocol (Alternative)

If you wish to manually verify the integration using the live compiled `vproxy` binary:

1. **Verify vproxy Installation**: Ensure `vproxy` is installed on your local path:
   ```bash
   which vproxy
   ```
2. **Run Cosmos Assistant in Dev Server mode**:
   ```bash
   task dev:backend
   ```
3. **Configure Settings**: Go to **Settings** -> **Model Settings**, set your proxy configurations (e.g. `socks5://127.0.0.1:1080`), and set a CLI model provider (e.g., `agy` or `gemini`).
4. **Trigger Generation**: Open the chat window, send a prompt, and check the backend logs to verify that the CLI tool execution was wrapped with `vproxy` transparently.

---

## 🧠 Testing the Domour Agent Framework

If you want to use `vproxy` to test or inspect the entire **Domour Agent Framework**, you can perform both **Targeted Component Tests** and **Full Daemon wrapping**:

### 1. Targeted Child-Process Interception (Recommended)
Since we implemented the dynamic `vproxy` wrapping directly inside `domour`'s CLI chat model adapter, `domour` will automatically wrap child executions of `agy` or `gemini` with `vproxy` whenever a proxy is configured.

- **How to test**:
  1. Start the `domour` daemon (either stand-alone or via `task dev:backend`).
  2. Configure a valid proxy URL in the settings.
  3. Send a chat request to a CLI-based agent (e.g. `agy`).
  4. Inspect the system's temporary directory to verify that `vproxy` ran and created temporary configuration logs:
     ```bash
     ls -la /tmp/vproxy-*.log
     tail -f /tmp/vproxy-*.log
     ```
  5. The output should confirm that the `vproxy` wrapper intercepted the `agy` process and routed all traffic through the proxy upstream.

### 2. Full Daemon Wrapping (Whole-system transparency)
If you want to run `domour` itself entirely behind `vproxy` so that *every* outbound network request (not just child processes, but also raw HTTP/gRPC/Database network calls) is transparently proxied:

- **How to test**:
  1. Ensure the background daemon is running or initialize the environment:
     ```bash
     sudo vproxy init
     ```
  2. Start the `domour` backend or entire application wrapped inside `vproxy`:
     ```bash
     vproxy -c /etc/vproxy/config.json ./build/bin/domour
     ```
      *(Or run the Wails server wrapper inside `vproxy`)*
  3. Under this mode, any external TCP/UDP connections made by `domour` will be captured by `vproxy` and transparently proxied according to your rules.

---

## ⚡ Super Fast Direct API-level Integration Testing

To drastically improve integration testing speed (10x faster than Playwright) and run real E2E chat request roundtrips through the Wails runtime RPC directly from the command line, use the **Direct API Tester** script:

### 1. Requirements
Ensure the Wails backend server is running in dev mode:
```bash
# Start backend dev server (automatically starts SurrealDB, NATS, and Domour)
ANTIGRAVITY_HARNESS_PATH=/home/qtopierw/workspace/projects/antigravity-sdk-go/bin/localharness task dev:backend
```

### 2. Run Direct API Tests
You can execute automated configuration-swap and chat tests using the custom testing utility:

- **Test `agy-cli` provider**:
  ```bash
  ./scripts/test_chat_api.py --provider agy-cli --message "Hello, who are you?"
  ```

- **Test `gemini-cli` provider over SOCKS5 proxy**:
  ```bash
  ./scripts/test_chat_api.py --provider gemini-cli --proxy socks5://127.0.0.1:1080 --message "Ping"
  ```

- **How it works**:
  1. It fetches the active settings via Wails RPC `GetSettings` (methodID `1440345669`) and backs them up.
  2. It updates settings with your test provider and proxy URL via Wails RPC `UpdateSettings` (methodID `1572524208`).
  3. It sends a message via Wails RPC `Chat` (methodID `809452495`), measuring exact latency.
  4. It automatically restores your original application settings after completion (or on error) to guarantee zero side-effects on your development workspace.

