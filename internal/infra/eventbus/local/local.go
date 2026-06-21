package local

import (
	"context"
	"sync"

	"github.com/qtopie/domour/internal/infra/eventbus"
)

type subscription struct {
	eb    *EventBus
	topic string
	ch    chan []byte
	done  chan struct{}
}

func (s *subscription) Unsubscribe() error {
	s.eb.mu.Lock()
	defer s.eb.mu.Unlock()

	subs := s.eb.subs[s.topic]
	for i, sub := range subs {
		if sub == s {
			// Remove from slice
			s.eb.subs[s.topic] = append(subs[:i], subs[i+1:]...)
			close(s.ch)
			close(s.done)
			break
		}
	}
	return nil
}

type EventBus struct {
	mu   sync.RWMutex
	subs map[string][]*subscription
}

func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[string][]*subscription),
	}
}

func (eb *EventBus) Publish(ctx context.Context, topic string, data []byte) error {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	subs := eb.subs[topic]
	for _, sub := range subs {
		// Non-blocking publish to avoid slow subscriber blocking the publisher
		select {
		case sub.ch <- data:
		case <-ctx.Done():
			return ctx.Err()
		default:
			// channel full, ignore or drop
		}
	}
	return nil
}

func (eb *EventBus) Subscribe(ctx context.Context, topic string, handler func([]byte)) (eventbus.Subscription, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub := &subscription{
		eb:    eb,
		topic: topic,
		ch:    make(chan []byte, 100),
		done:  make(chan struct{}),
	}

	eb.subs[topic] = append(eb.subs[topic], sub)

	go func() {
		for {
			select {
			case data, ok := <-sub.ch:
				if !ok {
					return
				}
				handler(data)
			case <-sub.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return sub, nil
}

func (eb *EventBus) Close() error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for topic, subs := range eb.subs {
		for _, sub := range subs {
			// safety check if already closed
			select {
			case <-sub.done:
			default:
				close(sub.ch)
				close(sub.done)
			}
		}
		delete(eb.subs, topic)
	}
	return nil
}
