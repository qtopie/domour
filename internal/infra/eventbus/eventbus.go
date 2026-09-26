package eventbus

import (
	infraspi "github.com/qtopie/domour/ark/spi/infra"
)

// Subscription represents an active subscription to a topic.
type Subscription = infraspi.Subscription

// EventBus defines the interface for event publishing and subscribing.
type EventBus = infraspi.EventBus
