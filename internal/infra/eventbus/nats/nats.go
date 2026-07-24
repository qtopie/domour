package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/qtopie/domour/internal/infra/eventbus"
)

type Config struct {
	URL           string
	StreamName    string
	SubjectPrefix string
}

type natsSubscription struct {
	consContext jetstream.ConsumeContext
}

func (s *natsSubscription) Unsubscribe() error {
	if s.consContext != nil {
		s.consContext.Stop()
	}
	return nil
}

type NatsEventBus struct {
	nc  *nats.Conn
	js  jetstream.JetStream
	cfg Config
}

func NewEventBus(cfg Config) (*NatsEventBus, error) {
	nc, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("failed to create jetstream context: %w", err)
	}

	ctx := context.Background()

	// Create or update the stream
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: []string{cfg.SubjectPrefix + ".>"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return &NatsEventBus{
		nc:  nc,
		js:  js,
		cfg: cfg,
	}, nil
}

func (eb *NatsEventBus) Close() error {
	if eb.nc != nil {
		eb.nc.Close()
	}
	return nil
}

func (eb *NatsEventBus) Publish(ctx context.Context, subject string, data []byte) error {
	fullSubject := fmt.Sprintf("%s.%s", eb.cfg.SubjectPrefix, subject)
	_, err := eb.js.Publish(ctx, fullSubject, data)
	return err
}

func (eb *NatsEventBus) Subscribe(ctx context.Context, subject string, handler func(data []byte)) (eventbus.Subscription, error) {
	fullSubject := fmt.Sprintf("%s.%s", eb.cfg.SubjectPrefix, subject)
	durableName := fmt.Sprintf("consumer_%s", subject)

	consumer, err := eb.js.CreateOrUpdateConsumer(ctx, eb.cfg.StreamName, jetstream.ConsumerConfig{
		Durable:       durableName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: fullSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		handler(msg.Data())
		_ = msg.Ack()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to consume: %w", err)
	}

	return &natsSubscription{consContext: cons}, nil
}

// Ensure NatsEventBus satisfies eventbus.EventBus interface
var _ eventbus.EventBus = (*NatsEventBus)(nil)
