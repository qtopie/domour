# 大脑组成与协作关系

本文件承载 `domour` 中与脑组件架构、组件协作、信号通路相关的设计内容。

## 1. 核心愿景：让智能体拥有“身体”

`domour` 的定位是为智能体提供一套仿生学架构的运行时环境。

传统的 LLM 开发往往只关注 Prompt 和 API 调用。对于 Agent Runtime 而言，更重要的是：

- 一个负责理解意图与规划任务的**大脑 (Cerebrum)**。
- 一个负责控制动作执行与技能输出的**小脑 (Cerebellum / Motor)**。
- 一个维持生存基础与消息通信的**脑干 (Brainstem)**。
- 一个负责信号分发与模型代理的**间脑 (Diencephalon)**。

---

## 2. 仿生架构层级 (Bionic Architecture)

`domour` 将 Agent 逻辑拆分为四个核心层级，明确了“认知”、“动作”、“基础”与“适配”的边界：

### 2.1 大脑 (Cerebrum): 认知与推断中枢
- **职责**: 负责思考、记忆、推理，决定“做什么”(What to do)。
- **特性**: 非确定性、高度智能化。Brain 的输出被视为“中间建议”，而非最终执行指令。
- **组成**: LLM 驱动、规则引擎、多级记忆（短期上下文 + 长期向量库）。

### 2.2 小脑 (Cerebellum / Motor): 战术与任务安全执行层
- **职责**: 接收 Brain 的意图，负责具体战术的编排与工具执行，决定“怎么做”(How to do)。
- **特性**: **确定性、高安全性**。小脑/Motor 持有最终输出校准权和工具调用权。
- **功能**: 执行 Tool/Skill，并对动作映射进行校验。

小脑连接脑干，单向连接给间脑反馈。

### 2.3 脑干 (Brainstem): 生命基础与信息传输层
- **职责**: 维持生命运转的底层基础。负责计算资源分配、跨节点通讯、状态持久化与事件总线，决定“在哪做”(Where to do)。
- **特性**: 稳定性、底层支持。基于 Dapr 实现，支持 Agent 节点的水平扩展与智能化协同。
- **安全设定**: 脑干的安全拦截屏障当前处于暂时降低状态（以优先保障执行通路与基础通信的连通性）。

### 2.4 间脑 (Diencephalon): 感觉传导与模型适配层 (一级架构)
- **职责**: 负责感觉信号（如传感器信号、遥测遥控信号）的路由与分发，并为上层提供统一的模型访问网关。
- **特性**: **无状态、适配性**。
- **功能**: 动态模型路由，屏蔽底层 LLM 差异，支持按需切换 Provider。

---

## 3. 概要设计：运行时核心实现与信号通路 (Synaptic & Runtime Design)

在代码实现层面，引擎通过 [Runtime](file:///home/qtopierw/workspace/projects/domour/internal/engine/core.go#L49-L54) 将四大神经元组件节点组装并连通。

### 3.1 核心节点定义
系统由四个并发运行的 Go 协程节点组成，节点之间通过带缓冲的 Channel 建立神经突触连接：
1. **[DiencephalonNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/diencephalon.go#L31-L36)**: 负责接收并路由输入刺激，管理 LLM 交互。
2. **[CerebrumNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/cerebrum.go#L35-L39)**: 驱动 System 2 慢速思考与异步记忆存取。
3. **[CerebellumNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/cerebellum.go#L53-L58)**: 运行 1kHz 快速时钟节拍，维护战术规划与动作编排。
4. **[BrainstemNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/brainstem.go#L103-L108)**: 维护动作执行循环、系统调用以及网络调度总线。

### 3.2 神经信号协作机制 (Neural Flow & Synaptic Wiring)

根据真实的仿生运动协作，系统设计了精细的信号通路。

#### 3.2.1 人脑运动协作信息流向图

```
========================================================================================
					  【人 脑 运 动 协 作 信 息 流 向 图】
========================================================================================

   ┌─────────────────────────────────────────────────────────────────────────┐
   │                                大 脑 皮 层                              │
   │                          （做出运动决策，下达总命令）                         │
   └────────────────────┬───────────────────────────────────▲────────────────┘
						│                                   │
						│ 1. 发出初始运动指令                  │ 5. 最终修正指令
						│                                   │   (微调肌肉)
						▼                                   │
   ┌────────────────────────────────────────┐               │
   │                 间 脑                  │               │
   │  (丘脑 Thalamus / 下丘脑 Hypothalamus) │               │
   │                                        │               │
   │  ┌──────────────────────────────────┐  │               │
   │  │           丘 脑 核 团            ├──┼───────────────┘
   │  │    (作为向上反馈的中继站/接待厅)    │  │ 4. 向上汇报修正数据
   │  └──────────────────────────────────┘  │
   └────────────────────────────────────────┘
						│
				  （解剖学上直连）
						▼
   ┌─────────────────────────────────────────────────────────────────────────┐
   │                                脑    干                                 │
   │    ┌───────────────────────────────────────────────────────────────┐    │
   │    │                       中脑 (Midbrain)                         │    │
   │    └──────────────────────────────┬────────────────────────────────┘    │
   │                                   │                                     │
   │                                   ▼ (主干道向下)                        │
   │    ┌──────────────────────────────┴────────────────────────────────┐    │
   │    │                       脑桥 (Pons)                             │    │
   │    │  (网络交换机：负责将大脑的运动命令“复制一份”，并进行分流)              │    │
   │    └──────────────┬────────────────────────────────┬───────────────┘    │
   │                   │                                │                    │
   │                   │ 2. 主路指令：继续向下            │ 2. 旁路监听：       │
   │                   │   (前往前线士兵)                │    把“命令复印件”   │
   │                   ▼                                │    横向送往小脑    │
   │    ┌────────────────────────────────────────┐      │                    │
   │    │               延髓 (Medulla)           │      │                    │
   │    └──────────────┬─────────────────────────┘      │                    │
   └───────────────────┼────────────────────────────────┼────────────────────┘
					   │                                │
					   │                                ▼
					   │               ┌─────────────────────────────────────┐
					   │               │               小  脑                │
					   │               │           (高级项目经理)            │
					   │               │                                     │
					   │               │ * 接收旁路监听的命令(理想)          │
					   │               │ * 接收身体传回的传感器数据(现实)      │
					   │               │ * 对比两者的误差，计算出纠偏方案     │
					   │               └────────────────┬────────────────────┘
					   │                                │
					   │                                │ 3. 向上递交“修正报告”  
					   │                                │   (通过小脑上脚)
					   │                                ▼
					   │                      （送往 间脑/丘脑 中转）
					   ▼
   ┌─────────────────────────────────────────────────────────────────────────┐
   │                               脊    髓                                  │
   │                       （将最终指令传导至全身肌肉）                             │
   └───────────────────────────────────┬─────────────────────────────────────┘
									   │
									   ▼
   ┌─────────────────────────────────────────────────────────────────────────┐
   │                       前 线 兵 营：全 身 肌 Muscle                         │
   │                    （执行动作，并实时向小脑汇报真实位置）                        │
   └─────────────────────────────────────────────────────────────────────────┘
```

#### 3.2.2 运行时信号通路 (Runtime Channels Wiring)

在 [routeNeuroSignals](file:///home/qtopierw/workspace/projects/domour/internal/engine/core.go#L107-L152) 协程的事件循环中，信号按照以下具体通路流转：

```mermaid
graph TD
	Input[外部感知输入] -->|SensorySignal| DN_Sensory[Diencephalon.RawSensoryIn]
    
	subgraph DiencephalonNode [间脑]
		DN_Sensory --> DN_Relay[SensoryRelay]
		DN_Relay -->|Thalamus Relay Up| DN_Semantic[SemanticOut]
		DN_Correction[来自小脑的纠偏修正输入] -->|Relay Up| DN_Semantic
	end
    
	subgraph CerebrumNode [大脑]
		DN_Semantic -->|CognitiveTask| CN_TaskIn[Cerebrum.TaskIn]
		CN_TaskIn -->|System 2 Thinking| CN_ResultOut[ResultOut]
	end

	CN_ResultOut -->|CognitiveResult 直连下发| DN_CommandIn[Diencephalon.RawSensoryIn/TactileOut]
    
	subgraph BrainstemNode [脑干 / 脑桥 Pons 分流与路由]
		DN_CommandIn -->|Pons Input| BS_CommandIn[Brainstem.CommandIn]
        
		BS_CommandIn -->|直连输出: respond| BS_Response[ResponseOut]
		BS_CommandIn -.->|旁路监听: 复制分流| Cebel_CognitiveIn[Cerebellum.CognitiveIn]
	end
    
	subgraph CerebellumNode [小脑 / Motor]
		Cebel_CognitiveIn -->|本地工具执行 / 1kHz Loop| Cebel_ActionOut[CorrectionOut]
		Cebel_ActionOut -->|本地反馈与纠偏计算| Cebel_ActionOut
	end

	Cebel_ActionOut -->|3. 纠偏修正报告 (小脑上脚)| DN_Correction
	BS_Response -->|ResponseOut| DN_ResponseIn[Diencephalon.ResponseIn]
	DN_ResponseIn -->|ResponseOut| Output[最终修正运动输出]
```

#### 3.2.3 信号分发规则
1. **直连指令下达**:
   - 大脑皮层 ([CerebrumNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/cerebrum.go#L35-L39)) 生成认知决策计划，通过间脑 ([DiencephalonNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/diencephalon.go#L31-L36)) 直接送达脑干 ([BrainstemNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/brainstem.go#L103-L108))。
2. **脑干路由与旁路监听 (Pons Splitting)**:
   - 脑干中的“脑桥 (Pons)”充当路由交换机，判断指令类型：
	 - 若为 `respond` 指令（最终结果输出），则直接送入 `ResponseOut` 投递给间脑进行外部输出。
	 - 若为工作流指令，则将“命令复印件”通过旁路同步分流给小脑 ([CerebellumNode](file:///home/qtopierw/workspace/projects/domour/internal/brain/cerebellum.go#L53-L58)) 的 `CognitiveIn` 频道。
3. **小脑本地执行与纠偏**:
   - **工具的调用直接在小脑（Motor）内部完成**。小脑通过内部的 [ToolExecutor](file:///home/qtopierw/workspace/projects/domour/internal/brain/cerebellum.go#L49-L52) 接口直接调用前线工具或执行 Skill，并在本地同步接收物理传感器反馈。
   - 小脑在 1kHz 的高频节拍下，通过对比**旁路监听到的预期命令**与**本地运行反馈的真实状态**计算偏差，生成纠偏动作。
4. **单行道修正反馈**:
   - 小脑得出的纠偏动作并不原路返回脑干，而是向上发射至间脑（丘脑核团），再由间脑直连转交给大脑进行高层决策微调，从而实现动态的自适应调节闭环。

---

## 4. 自省能力 (Reflection)

作为 Runtime，`domour` 提供了内置的自省闭环：
- **过程自省 (ReAct)**: 小脑实时监控工具执行反馈，本地动态调整或回滚战术动作。
- **认知自检 (Verification)**: 大脑在做出重大决策或输出最终答复前，进行一致性自检或依赖沙箱测试验证结果。

---

## 5. 认知推理模式 (Cognitive Reasoning Modes)

Domour 在间脑 (Diencephalon) 与会话状态管理器中内置了三种主流的认知推理模式，分别对应不同的能效与复杂度要求：

### 5.1 规划与执行模式 (Plan & Execute)
* **标识**: `plan_execute` / `plan_execute_nested`
* **工作原理**:
  - 大脑 (Cerebrum) 负责对用户目标进行宏观步骤拆分（形成步骤 DAG/列表）。
  - 大脑将计划写入会话状态机，小脑 (Cerebellum) 顺序监听计划节点，在本地执行工具，并将结果逐步写回至步骤状态中，直至所有计划步骤执行完毕并由间脑返回。
* **适用场景**: 复杂的任务拆解及大颗粒步骤的流式编排。

### 5.2 全局 ReAct 模式 (ReAct Mode)
* **标识**: `react`
* **工作原理**:
  - 采用经典的 Thought -> Act -> Observe 闭环。
  - 大脑 (Cerebrum) 负责每一次的 Thought（即大模型推理和工具选择）。
  - 间脑协调器 (Diencephalon Coordinator) 将决策中选择的工具派发给小脑 (Cerebellum) 运行 (Act)。
  - 小脑执行后将结果 (Observation) 作为 `EventExecResult` 送回间脑事件总线，追加至历史上下文，并再次激活大脑进行下一步 Thought，直至大脑输出结束指令 (`respond`)。
* **适用场景**: 需要频繁依靠外部环境状态做即时决策和反馈的探索式任务。

### 5.3 全局 Simple 模式 (Simple Mode)
* **标识**: `simple`
* **工作原理**:
  - 针对轻量简单场景的高效单次调用模式。
  - 间脑不作本地规划，直接把用户请求转发给大脑。
  - 大脑 (Cerebrum) 仅进行**一次**认知推理，直接输出最终的结果模板或草稿响应。
  - 小脑 (Cerebellum) 拦截这一结果并进行校对校验 (`verify`)：
    - **无问题**: 小脑确认输出正确，直接指示间脑完成会话并返回，将 LLM 调用限制在仅有的一次。
    - **有问题**: 若校验到错误或内容缺陷，小脑可根据情况选择调用本地工具（如 calculator 等）辅助获取正确数值，并生成 `correction`（纠偏反馈）报告，通过间脑事件总线再次上报给大脑，引导大脑进行重新生成与二次微调。
* **适用场景**: 日常问答、简易工具绑定以及低 token 消耗、低时延的高效执行场景。
