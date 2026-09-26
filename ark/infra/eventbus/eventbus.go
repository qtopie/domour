package eventbus

import (
	infraspi "github.com/qtopie/domour/ark/spi/infra"
)

// Subscription represents an active subscription to a topic that can be cancelled.
type Subscription = infraspi.Subscription

// EventBus defines the interface for publish/subscribe message dispatch.
type EventBus = infraspi.EventBus
