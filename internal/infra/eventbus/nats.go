package eventbus

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	URL           string
	StreamName    string
	SubjectPrefix string
}

type EventBus struct {
	nc *nats.Conn
	js jetstream.JetStream
	cfg Config
}

func NewEventBus(cfg Config) (*EventBus, error) {
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

	return &EventBus{
		nc:  nc,
		js:  js,
		cfg: cfg,
	}, nil
}

func (eb *EventBus) Close() {
	eb.nc.Close()
}

func (eb *EventBus) Publish(ctx context.Context, subject string, data []byte) error {
	fullSubject := fmt.Sprintf("%s.%s", eb.cfg.SubjectPrefix, subject)
	_, err := eb.js.Publish(ctx, fullSubject, data)
	return err
}

func (eb *EventBus) Subscribe(ctx context.Context, subject string, handler func(jetstream.Msg)) (jetstream.ConsumeContext, error) {
	fullSubject := fmt.Sprintf("%s.%s", eb.cfg.SubjectPrefix, subject)
	
	// Create a consumer
	// Note: Durable name is fixed here, meaning all instances will share the workload for this subject.
	// If you want broadcast, you need unique durable names or ephemeral consumers.
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
		handler(msg)
		msg.Ack()
	})
    if err != nil {
        return nil, fmt.Errorf("failed to consume: %w", err)
    }

	return cons, nil
}
