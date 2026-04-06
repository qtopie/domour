package brainstem

import "context"

// TaskDispatcher 作为依赖倒置的借口，交由 cosmos-star 去实现
// 负责把小脑打包好的逻辑任务下发给具体的物理或边缘节点进行计算
type TaskDispatcher interface {
	// Dispatch 下发给 cosmos-star 集群处理，返回底层系统生成的 JobID
	Dispatch(ctx context.Context, job JobPayload) (string, error)

	// WatchTask 监听这批任务在某个计算节点的执行流和结果
	WatchTask(ctx context.Context, jobID string) (<-chan JobResult, error)
}

// ClusterMonitor 集群与设备健康监控，由 cosmos-star 拓扑网络提供
type ClusterMonitor interface {
	// GetNodeStatus 获取当前星系/集群内可用的计算节点和设备
	GetNodeStatus(ctx context.Context) ([]NodeInfo, error)

	// Ping 报告 domour 目前的存活与健康状态给 cosmos-star 脑干网关
	Ping(ctx context.Context, status AgentStatus) error
}

// EventBus 全局事件通信总线接口（发布订阅）
type EventBus interface {
	Publish(ctx context.Context, topic string, event []byte) error
	Subscribe(ctx context.Context, topic string, handler func(event []byte)) error
}

// PersistentStorage 底层的统一数据持久化接口（数据库/KV等，由基础平台注入）
type PersistentStorage interface {
	SaveState(ctx context.Context, key string, data []byte) error
	LoadState(ctx context.Context, key string) ([]byte, error)
}
