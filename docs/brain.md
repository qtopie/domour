## 1. 核心架构：自适应多级调度

框架将任务处理分为三层路径，根据 **复杂度评估 (Observer)** 结果动态分流：

* **L1 (简单):** 规则或单次 Tool 调用，追求零延迟。
* **L2 (一般):** 线性 Chain，处理标准化的多步流程。
* **L3 (复杂):** 采用 **Planner-Worker** 模式，进入状态循环。

---

## 2. 关键模式：递归式 Planner-Worker

这是框架的“大脑”逻辑：

* **Planner (PM):** 负责将大目标拆解为 `TaskSteps`。如果子任务依然复杂，则**递归调用**框架自身进行二次拆解。
* **Worker (执行者):** 具体的原子能力（工具、API、代码执行）。
* **Observer (监考官):** 实时监控 Worker 输出。若报错或发现用户新输入，立即打断执行，并将当前快照回传给 Planner 进行**增量重规划 (Re-planning)**。

---

## 3. 状态管理：树状上下文 (State Tree)

为了支撑“执行中修改”和“断点续传”，状态存储设计为三层：

* **短期 (Eino State):** 节点间传递的实时上下文。
* **中期 (Redis/DB):** 每一个关键步骤的 `ExecutionLog` 持久化快照。
* **长期 (Vector DB):** 历史任务的成功编排路径，作为后续 Planner 的 Few-shot 经验。

---

## 4. 工程实践与规范

* **项目结构:** 采用标准的 Go 项目布局，核心逻辑放在 `pkg/brain`（对外开放接口）和 `internal/`（私有算法）。
* **Eino 深度集成:** * 利用 `compose.Graph` 实现带环路的自适应逻辑。
* 通过 `WithState` 管理层级化任务流。
* 利用 `Aspect`（切面）注入持久化和监控。



---

## 5. 设计的亮点总结

| 维度 | 传统编排 | 我们的设计 (Eino-Brain) |
| --- | --- | --- |
| **灵活性** | 静态 DAG，一旦开始无法回头。 | **动态流转**，支持执行中根据反馈重画图谱。 |
| **容错性** | 出错即停止。 | **自适应纠偏**，基于 Log 自动生成补丁计划。 |
| **扩展性** | 代码耦合。 | **插件化 Worker**，支持递归嵌套子 Agent。 |


```d2
direction: right
theme: 200

# 1. 入口与分流层
Dispatcher: 复杂度评估器 (Observer) {
  shape: diamond
  label: "Task Complexity?\n(Cost/Latency/Logic)"
}

# 2. 三级执行路径
# --- L1: 简单路径 ---
L1_Path: L1 (Simple) {
  style: { stroke: "#4caf50"; stroke-dash: 5 }
  Router: 规则引擎 (Rules)
  Tool: 单次 Tool 调用
  
  Router -> Tool: 匹配命中
}

# --- L2: 一般路径 ---
L2_Path: L2 (Standard) {
  style: { stroke: "#2196f3" }
  Chain: 线性 Chain (Linear) {
    Step1 -> Step2 -> Step3
  }
}

# --- L3: 复杂路径 (Cosmos Star 核心) ---
L3_Path: L3 (Complex Brain) {
  style: { stroke: "#9c27b0"; stroke-width: 3 }
  
  Planner: 递归规划者 (PM)
  Worker: 原子执行者 (Executor)
  Monitor: 实时监考官 (Observer)

  Planner -> Worker: 任务拆解
  Worker -> Monitor: 结果反馈
  Monitor -> Planner: 动态重规划 (Loop)
  
  # 递归：子任务过大时重回 Planner
  Planner -> L3_Path: 递归调用自身
}

# 3. 支撑层
State_Tree: 状态树 (Eino State) {
  Short_Term: 实时上下文
  Mid_Term: 持久化快照 (Redis)
}

# 连线关系
Dispatcher -> L1_Path: "低复杂度\n(零延迟追求)"
Dispatcher -> L2_Path: "中等/标准化\n(多步流程)"
Dispatcher -> L3_Path: "高复杂度\n(状态循环/未知)"

L1_Path -> State_Tree.Short_Term
L2_Path -> State_Tree.Short_Term
L3_Path -> State_Tree.Mid_Term: "关键步骤快照"
```