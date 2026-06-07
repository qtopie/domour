# Skill Schema 与 Skill / Plugin 关系

本文档定义 Skill、Plugin、Tool、Memory 在 agent 中的角色边界。

## 1. 核心定义

### Skill

Skill 是 **任务级编排契约**，回答下面几个问题：

- 什么时候应该使用我
- 我依赖哪些工具或服务
- 我需要什么上下文
- 我是否需要 UI
- 我有哪些确认点与风险等级

Skill 不等于插件，也不等于某个具体模型 prompt。

### Plugin

Plugin 是 **UI 或专用执行扩展**。

它负责：

- 呈现复杂领域界面
- 承载特定交互流程
- 暴露受控的后端方法

Plugin 不负责全局路由决策。

### Tool

Tool 是 **原子执行能力**，例如：

- MCP tool
- HTTP API
- gRPC service
- 本地命令
- 数据库查询

### Memory

Memory 是 Skill 的辅助上下文，不是 Skill 本身。

它可以包含：

- 用户偏好
- 历史成功路径
- 失败反例
- 风险提示

## 2. 关系模型

推荐关系如下：

- `Skill -> many Tools`
- `Skill -> zero or one primary Plugin`
- `Plugin -> many exposed methods`
- `Skill -> many Memory records`

这意味着：

- 一个 Skill 可以纯后端执行，无需 Plugin
- 一个 Plugin 可以服务多个 Skill
- Tool 不应该被前端任意直连

## 3. 最小 Skill Schema

建议的最小字段：

```json
{
  "id": "devops.k8s.pod-manager",
  "name": "K8s Pod Manager",
  "description": "查看、重启和排查 Pod 状态",
  "intent_tags": ["k8s", "pod", "ops"],
  "inputs": {
    "required": ["cluster"],
    "optional": ["namespace", "pod_name"]
  },
  "tools": ["k8s.getPods", "k8s.restartPod"],
  "plugin": {
    "id": "com.qtopie.k8s.dashboard",
    "mode": "optional"
  },
  "risk": {
    "level": "medium",
    "requires_confirmation": true
  },
  "memory_policy": {
    "allow_preferences": true,
    "allow_examples": true,
    "allow_negative_feedback": true
  }
}
```

## 4. Skill 生命周期

1. Router 选择候选 Skill
2. Planner 为 Skill 组装上下文
3. Skill 调用 Tool 或请求打开 Plugin
4. 执行结果进入审阅 / 确认
5. 经验写入 Memory

## 5. 与当前插件 manifest 的关系

当前仓库里的插件 manifest 仅适合描述运行时插件：

- `id`
- `name`
- `version`
- `entrypoint`
- `methods`

它不足以表达：

- 触发条件
- 风险等级
- 所需上下文
- 记忆策略
- 与 Tool 的映射

因此后续应新增 **Skill registry**，不要继续把插件 manifest 直接扩展成全部 agent 语义。

## 6. 关于“神经网络”在 Skill 体系中的位置

如果要引入神经网络概念，最适合放在 Skill 体系的这几个位置：

- **向量表示**：为 Skill、会话、反馈建立语义表示
- **候选排序**：为多个 Skill 生成优先级
- **风险评分**：为动作给出置信度与审阅优先级
- **偏好预测**：根据用户历史选择默认参数

它不应替代：

- Skill schema
- permission model
- audit trail
- confirmation rules

这些必须继续保持显式、可检查、可回放。
