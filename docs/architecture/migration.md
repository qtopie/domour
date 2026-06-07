# 架构迁移路线（Migration Roadmap）

本文档描述如何从当前实现演进到目标架构。

## 阶段 0：先把概念边界固定

目标：

- 停止混用“当前实现”和“目标愿景”
- 固定 Skill / Plugin / Tool / cosmos-star 的语义边界

输出：

- `current.md`
- `target.md`
- `../features/plugins/skill-schema.md`
- `../features/plugins/security.md`

## 阶段 1：Skill registry 落地

目标：

- 在插件系统之外建立正式 Skill 注册表
- 让 Skill 不再等于插件 manifest

需要新增的最小能力：

1. Skill schema
2. Skill registry
3. Skill selector / router
4. Skill 与 Plugin 的映射关系

建议规则：

- 一个 Skill 可以没有 Plugin
- 一个 Plugin 可以被多个 Skill 复用
- Tool 不直接暴露给 UI，由 Skill / host 控制调用

## 阶段 2：插件安全模型落地

目标：

- 把 iframe 嵌入升级为真正的受限宿主模型

需要新增的最小能力：

1. iframe sandbox 策略
2. Host <-> Plugin bridge 协议
3. Plugin permission manifest
4. 审计事件与确认点

完成标准：

- 插件不能直接拿到宿主任意能力
- 每次敏感调用都有可记录的入口

## 阶段 3：反馈与经验系统

目标：

- 从“有日志”升级到“有结构化经验”

需要新增的最小能力：

1. 反馈事件模型
2. 偏好与反例存储
3. 经验注入规则
4. 失效与清理策略

建议原则：

- 默认记录显式反馈
- 隐式反馈必须保守解释
- 经验更新不能绕过审计

## 阶段 4：cosmos-star 增强接入

目标：

- 把 cosmos-star 作为可插拔增强层重新接回系统

前提：

- 启动协议稳定
- 健康检查明确
- 错误恢复路径可定义

设计要求：

- 没有 cosmos-star 时，单机版仍可运行
- 有 cosmos-star 时，新增节点发现、事件总线、远程任务等增强能力
- 所有依赖都必须可降级

## 阶段 5：神经网络驱动的路由优化

目标：

- 在协议稳定之后，再把学习型模型引入 Skill 选择与风险评估

适合引入的位置：

- 候选 Skill 排序
- 历史轨迹召回
- 风险评分
- 个性化偏好预测

不适合优先引入的位置：

- 插件权限控制
- 核心审计链路
- 高风险确认逻辑

这些基础约束应继续由显式规则保证。
