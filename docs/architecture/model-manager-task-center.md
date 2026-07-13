# Distributed Model Management Architecture

This document describes the design and implementation of the Model Manager's transition from a local-only service to a distributed task-orchestrated system using the `cosmos-star` cluster.

## 1. Core Concept
Model downloading is no longer treated as a simple API call. Instead, it is treated as a **Cluster Infrastructure Task** with a defined lifecycle:
- **Declarative Installation**: Handled by Pulumi to ensure idempotency and state tracking.
- **Procedural Verification**: Handled by Taskfile to confirm the environment state post-installation.
- **Distributed Execution**: Capable of being routed to specific nodes in the cluster.

## 2. Technical Stack
- **Backend (Go)**: Constructs an `L2Task` (Explicit Plan) and submits it to the `cosmos-star` gRPC gateway.
- **Engine 1 (Pulumi)**: Uses the `pulumi-command` provider to execute `llama-cli pull`. It manages retries and concurrency locks.
- **Engine 2 (Taskfile)**: Executes dynamic validation scripts (e.g., `llama-cli list | grep`) to verify success.
- **Frontend (React)**: Polls task status and uses regex to parse CLI stdout into structured progress data.

## 3. Task Workflow (Sequence)
1.  **UI Trigger**: User clicks "Download" on a model (e.g., `phi4`).
2.  **Plan Construction**: `models.Service` builds a gRPC `SubmitTaskRequest` with:
    -   `SubTask 1`: Engine `pulumi`, Method `up`, Script `llama-cli pull phi4`.
    -   `SubTask 2`: Engine `taskfile`, DependsOn `SubTask 1`, Script `llama-cli list | grep phi4`.
3.  **Cluster Submission**: Task is sent to the `cosmos-star` node (port 50061).
4.  **Polling**: Frontend polls `GetInfraTaskStatus`.
5.  **Progress Extraction**: Frontend parses the incoming `stdout` stream:
    -   Regex: `/(\d+)%\s+▕/`
    -   Matches `pulling 4e30e2665218: 13% ▕████`
6.  **Completion**: Once SubTask 2 passes, the model is marked as `Ready` in the UI.

## 4. Current Implementation Details
- **Location**: `pkg/models/service.go` (`PullOllamaModel` method, which now delegates to Llama.cpp under the hood).
- **Frontend**: `frontend/src/pages/ModelManager/index.tsx` (Polling & Regex logic).
- **Validation**: Uses inline Taskfile YAML strings to avoid dependency on local files.

## 5. Future Scalability (Roadmap)
- [ ] **Node Selection**: Add a dropdown in the UI to select which cluster node should receive the model.
- [ ] **Multi-Node Progress**: Aggregate progress logs if a model is being deployed to multiple nodes simultaneously.
- [ ] **Automatic Clean-up**: Use Pulumi `destroy` logic to uninstall models and reclaim disk space via the same task pipeline.
- [ ] **Catalog Sync**: Synchronize the `default_catalog.json` with a remote repository to suggest new models without requiring a full application update.

---
*Documented on May 10, 2026*
