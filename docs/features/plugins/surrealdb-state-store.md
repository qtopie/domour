# 方案设计：基于 SurrealDB 的 Dapr 可插拔状态存储组件

## 1. 背景与动机 (Background & Motivation)

在 PC 客户端和边缘嵌入式（Edge/Embedded）场景下，系统的资源（内存、CPU、磁盘空间）和运维复杂度受到严格限制。
Domour 智能体在业务层已经深度依赖 **SurrealDB** 作为其认知图谱（图查询）、长期记忆（向量召回）以及会话管理（Document 存储）的底座。

如果为了让 Dapr 运行 Workflow 和 Actor，又强行引入 Redis、PostgreSQL 或额外的 SQLite，会导致：
1.  **组件冗余**：嵌入式设备上运行多个不同类型的数据库，增加资源占用。
2.  **运维麻烦**：需要多管理一个数据库的生命周期、备份及配置。

### 1.1 架构决策权衡 (Architecture Decision Record / ADR)

为了满足 Dapr 运行时对“支持事务的状态存储”的刚需，我们对以下技术选型方案进行了权衡分析：

| 选型方案 | 适用场景 | 优势 | 劣势/放弃原因 |
| :--- | :--- | :--- | :--- |
| **方案 A: 引入 SQLite** | 单机/单节点边缘端 | 极轻量，进程内运行，无需额外部署容器。 | 1. 造成“双数据库后端 (SurrealDB + SQLite)”组件冗余。<br>2. 仅支持单机，**无法在多节点分布式集群中提供状态共享**。 |
| **方案 B: 引入 Redis/PostgreSQL** | 分布式云端 | 分布式高可用，官方组件极其成熟。 | 对于 PC 桌面端和低算力嵌入式设备，资源开销过高，部署和运维极其笨重。 |
| **方案 C: 自研 Dapr Pluggable Component (已采纳)** | **PC / 嵌入式 / 云端通用** | **1. 统一存储栈**：仅需运行一个 SurrealDB，同时满足业务图查询和 Dapr 状态持久化。<br>**2. 分布式支持**：SurrealDB 本身支持多节点部署（基于 TiKV 后端），能天然作为分布式状态存储组件。<br>**3. 零组件冗余**：消除嵌入式环境下多余的数据库。 | 需自研轻量级 gRPC 适配器（Pluggable Component），有少量前期开发成本。 |

**决策结论**：出于在 **PC/嵌入式场景中追求技术栈最简和最低资源开销** 的考虑，Domour 决定支持 **方案 C**，即通过可插拔组件标准将 SurrealDB 映射为 Dapr 状态存储。

**目标**：开发一个轻量级的 **Dapr 可插拔状态存储组件（Dapr Pluggable State Store Component）**，让 Dapr 的 State Store 协议直通 SurrealDB。从而在 PC 和嵌入式设备上**仅运行一个 SurrealDB 实例**，同时满足业务层高级查询与 Dapr 状态持久化的需求。


---

## 2. 核心架构设计 (Core Architecture)

Dapr 1.9+ 提供了 **Pluggable Components（可插拔组件）** 规范。它允许开发者通过 gRPC 协议和本地 Unix Domain Socket (UDS) 向 Dapr 注册自定义组件，而无须修改 Dapr 核心源码或重新编译 `daprd`。

```
┌────────────────────────────────────────────────────────┐
│                      Domour App                        │
│ ┌───────────────────────────┐ ┌──────────────────────┐ │
│ │  Cerebrum (业务图查询)    │ │   Cerebellum (Actor) │ │
│ └─────────────┬─────────────┘ └──────────┬───────────┘ │
└───────────────┼──────────────────────────┼─────────────┘
                │                          │
        (SurrealQL Direct)            (Dapr SDK)
                │                          │
                ▼                          ▼
      ┌───────────────────┐      ┌───────────────────┐
      │  SurrealDB Server │      │   Dapr Sidecar    │
      └─────────▲─────────┘      └─────────┬─────────┘
                │                          │
         (SQL over WS/gRPC)           (gRPC over UDS)
                │                          │
      ┌─────────┴──────────────────────────▼─────────┐
      │    Domour SurrealDB Pluggable Component      │
      │   (Implements dapr.proto.components.v1)      │
      └──────────────────────────────────────────────┘
```

### 运行机制
1.  **统一后端**：系统启动一个轻量级的 SurrealDB 守护进程（或以嵌入式库运行）。
2.  **可插拔注册**：Domour 启动一个伴随进程（或在主进程内作为子协程），实现 Dapr 的 `StateStore` gRPC 接口，并在本地创建 Unix Socket 文件（例如 `/tmp/dapr-components/state-surrealdb.sock`）。
3.  **Dapr 载入**：Dapr Sidecar 启动时读取配置，通过 Unix Socket 发现并直连该组件，将其注册为名为 `state.surrealdb` 的 State Store。
4.  **状态路由**：Dapr 内部的工作流与 Actor 状态读写，最终都通过该可插拔组件，映射为对 SurrealDB 内 `dapr_state` 表的 SQL 操作。

---

## 3. gRPC 接口映射设计 (gRPC Interface Mapping)

组件需要实现 Dapr 的 `dapr.proto.components.v1.StateStore` 接口，具体映射规则如下：

### 3.1 初始化 (`Init`)
*   **动作**：连接到本地或指定的 SurrealDB 实例，完成鉴权。
*   **建表准备**：确保在数据库中创建了用于存放状态的表（默认表名：`dapr_state`），并为 `key` 创建唯一索引。
    ```sql
    DEFINE TABLE dapr_state SCHEMALESS;
    DEFINE INDEX dapr_key ON TABLE dapr_state COLUMNS key UNIQUE;
    ```

### 3.2 特性声明 (`Features`)
*   **动作**：声明组件支持的特性。
*   **必须声明**：
    *   `FEATURE_ETAGS`：支持版本并发控制（乐观锁）。
    *   `FEATURE_TRANSACTIONAL`：**必须支持**。由于 Dapr Workflow 和 Actor 依赖批量事务写入状态与历史，SurrealDB 必须暴露此特性。

### 3.3 读取 (`Get`)
*   **输入**：`GetRequest{ key: "actor||agent1||state" }`
*   **SQL 映射**：
    ```sql
    SELECT value, etag FROM dapr_state WHERE key = $key LIMIT 1;
    ```
*   **处理**：若记录存在，返回 Value 字节流及 ETag 版本号。

### 3.4 写入 (`Set`)
*   **输入**：`SetRequest{ key, value, etag, metadata }`
*   **SQL 映射**：
    *   **常规写入**：
        ```sql
        UPSERT dapr_state SET value = $value, etag = $new_etag WHERE key = $key;
        ```
    *   **带乐观锁写入 (ETag)**：
        ```sql
        -- 仅当旧 etag 匹配时才更新
        UPDATE dapr_state SET value = $value, etag = $new_etag WHERE key = $key AND etag = $old_etag;
        ```

### 3.5 删除 (`Delete`)
*   **输入**：`DeleteRequest{ key, etag }`
*   **SQL 映射**：
    ```sql
    DELETE FROM dapr_state WHERE key = $key;
    ```

### 3.6 事务处理 (`Transact`)
*   **说明**：Dapr 传入一组 `ExecuteStateTransactionRequest`（包含多个 `Set` 和 `Delete` 操作）。
*   **SQL 映射**：SurrealDB 支持用 `BEGIN TRANSACTION` 进行强一致性包裹：
    ```sql
    BEGIN TRANSACTION;
      -- 循环执行事务中的每个操作
      UPSERT dapr_state SET value = $val1 WHERE key = $key1;
      DELETE FROM dapr_state WHERE key = $key2;
    COMMIT TRANSACTION;
    ```

---

## 4. 配置文件配置示例 (Dapr Component Config)

可插拔组件编写完成后，在 Dapr `components` 目录下配置如下组件声明：

```yaml
apiVersion: dapr.io/v1alpha1
kind: Component
metadata:
  name: statestore
spec:
  type: state.surrealdb # 对应 UDS socket 文件的名称
  version: v1
  metadata:
    - name: address
      value: "ws://localhost:8000"
    - name: username
      value: "root"
    - name: password
      value: "root"
    - name: namespace
      value: "domour"
    - name: database
      value: "state"
```

---

## 5. 实施路线图 (Implementation Roadmap)

1.  **定义可插拔组件骨架**：在 `internal/infra/dapr/pluggable` 目录下，使用 `github.com/dapr/go-sdk/components/state` 编写可插拔组件的核心逻辑。
2.  **Go 实现与测试**：
    *   封装对 `internal/infra/storage/surreal.go` 的调用，实现状态转换。
    *   在单机环境启动 SurrealDB，并通过 UDS 将其暴露给本地的 `daprd`。
3.  **验证 DurableAgent**：切换 `components/statestore.yaml` 的实现为 `state.surrealdb`，运行 `durable_agent_test.go` 验证在发生崩溃重启后，状态能否正常从 SurrealDB 恢复并继续运行工作流。

---

## 6. 扩展展望：作为 Dapr 向量存储 (Vector Store / RAG)

除了状态存储 (State Store) 之外，Agent 的**检索增强生成 (RAG)** 和**长期语义记忆**还需要一个高效率的**向量数据库 (Vector DB)**。

### 6.1 SurrealDB 的向量支持特性 (Vector Capabilities)
SurrealDB 从 v1.0.0 开始就原生支持向量类型和索引：
1.  **定义向量字段**：可在表中定义指定维度和浮点类型的数组。
    ```sql
    DEFINE FIELD embedding ON TABLE agent_memory TYPE array<number, 1536>;
    ```
2.  **创建 HNSW/M-Tree 向量索引**：使用内置的向量索引提升语义搜索速度（支持 Cosine, Euclidean, Manhattan 距离）。
    ```sql
    DEFINE INDEX vector_idx ON TABLE agent_memory 
      FIELDS embedding 
      MTREE DIMENSION 1536 DISTANCE COSINE;
    ```
3.  **向量与图混合检索 (Hybrid Search)**：利用 SurrealQL 执行极具威力的混合查询。例如：先通过图关系查出“该 Agent 的朋友圈”，再在此朋友圈的范围内做语义向量召回。
    ```sql
    -- 先找出关系好友，再进行向量相似度过滤
    SELECT *, vector::similarity::cosine(embedding, $query_vector) AS score 
    FROM agent_memory 
    WHERE ->knows->agent.id = agent:john AND embedding < 1536 > $query_vector 
    ORDER BY score DESC LIMIT 5;
    ```

### 6.2 选型收益：真正的“单存储三维合一” (Unified Storage Stack)

通过将 SurrealDB 同时作为 **图查询数据库**、**Dapr 状态库 (State Store)** 以及 **向量存储库 (Vector Store)**，Domour 框架在 PC 和嵌入式场景中实现了极致的最简技术栈：

```
             ┌────────────────────────────────────┐
             │            SurrealDB               │
             │       (Single Binary / Pod)        │
             ├───────────────┬────────────────────┤
             │               │                    │
             ▼               ▼                    ▼
     [ 1. 图关系网 ]    [ 2. 状态与工作流 ]   [ 3. 向量与语义记忆 ]
      (Graph Store)      (Dapr State Store)     (Vector Store)
      - Agent实体关系图    - Actor 运行状态       - RAG 文档召回
      - 组织架构与拓扑     - Workflow 步骤记录    - 长期语义经验库
```

### 6.3 落地策略
1.  **业务层语义召回**：通过原生 SDK 发送 `surrealQL` 直接调用 SurrealDB 的向量索引和图联查，以获取最高性能。
2.  **Dapr 外设集成**：未来如果 Dapr 的 `bindings` 或 `vectorstore` 组件标准成熟，我们也可以采用类似 Pluggable Component 的方式，将 SurrealDB 包装成标准的 Dapr 向量存储组件，以兼容其他基于 Dapr 的智能体（Dapr Agents）生态。

