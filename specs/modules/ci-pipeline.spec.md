# Module Spec: CI Pipeline & Automated Verification (`.github/workflows/ci.yml`)

## 1. Overview
本规范定义 Domour 仓库的 GitHub Actions 持续集成 (CI) 工作流规范，负责自动化代码编译、全量单元与集成测试、Spec-Drift 漂移校验、Harness 评估沙盒验证以及跨平台/关键入口二进制构建打包，确保主干分支及 Pull Request 的工程质量与稳定性。

## 2. Pipeline Architecture & Job Contracts

### 2.1 Trigger Events
- **Push**: `main` 分支。
- **Pull Request**: 针对 `main` 分支的 PR（包含 `opened`, `synchronize`, `reopened`）。
- **Workflow Dispatch**: 支持手动触发执行。

### 2.2 Execution Matrix & Steps
工作流包含三个阶段/作业：
1. **verification (测试与质量验证门禁)**:
   - Runner: `ubuntu-latest`
   - Setup Go: `go-version-file: go.mod` (Go 1.25.x / 1.26.x 兼容)
   - Cache: Go modules & build cache (`actions/setup-go` 自动缓存)
   - Step 1: **Harness & Spec Sandbox Validation**: 执行 `./scripts/check.sh`
   - Step 2: **Unit & Integration Tests**: 执行 `go test -v -race -covermode=atomic ./...` (带竞态检测)
   - Step 3: **Go Vet Check**: 执行 `go vet ./...`
2. **build (多目标二进制编译构建)**:
   - Runner: `ubuntu-latest`
   - Dependencies: `verification` 通关后执行
   - Build Targets:
     - CLI 主二进制: `cmd/main.go` -> `bin/domour`
     - LlamaCpp In-Tree CGO 构建验证（可选/容错检查）: 确保 pure-Go stub 默认编译和运行无故障
3. **artifacts (构建产物归档)**:
   - 上传 `bin/domour` 作为 Actions Artifact，便于版本追溯和快速下载验证。

## 3. Acceptance Criteria (BDD)

### Feature: CI 自动化工作流与门禁检验

#### Scenario 1: [SPEC-CI-001] Workflow 配置文件完备性与语法合规
- **Given** `.github/workflows/ci.yml` 配置文件
- **When** 解析 YAML 结构
- **Then** 包含合法触发器 (`push`, `pull_request`, `workflow_dispatch`)、`setup-go` 动作及 `./scripts/check.sh` 门禁步骤
- **Mapped Test:** `tests/ci_workflow_test.go:TestCIWorkflowFile_StructureAndSteps`

#### Scenario 2: [SPEC-CI-002] Harness 与全量测试脚本执行链路完整
- **Given** 本地执行与 CI 一致的验证流水线命令序列
- **When** 运行 `./scripts/check.sh`、`go vet ./...` 与 `go test ./...`
- **Then** 所有步骤零退出码退出，无 Spec 漂移，无竞态死锁
- **Mapped Test:** `tests/ci_workflow_test.go:TestLocalVerificationChain`

#### Scenario 3: [SPEC-CI-003] 二进制编译与入口可执行性
- **Given** 源码处于 clean 状态
- **When** 执行 `go build -o bin/domour ./cmd`
- **Then** 生成可执行二进制文件 `bin/domour`，运行 `bin/domour --help` 成功返回并退出码为 0
- **Mapped Test:** `tests/ci_workflow_test.go:TestBinaryBuildAndHelpExecution`
