package eventbus

import "context"

// Subscription represents an active subscription to a topic.
type Subscription interface {
	Unsubscribe() error
}

// EventBus defines the interface for event publishing and subscribing.
type EventBus interface {
	Publish(ctx context.Context, topic string, data []byte) error
	Subscribe(ctx context.Context, topic string, handler func(data []byte)) (Subscription, error)
	Close() error
}
