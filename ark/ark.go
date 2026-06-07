// Package ark provides the unified public SDK for Domour.
// It serves as the primary gateway for external integrations, aggregating
// resource management (Hub) and system governance (Governor).
package ark

import (
	"github.com/qtopie/domour/ark/governor"
	"github.com/qtopie/domour/ark/hub"
)

// Ark is the unified gateway to Domour's management and governance systems.
// It aggregates the Hub (for resource registration) and the Governor
// (for high-level system state and mode regulation).
type Ark interface {
	hub.ArkHub
	governor.Governor
}

type combinedArk struct {
	hub.ArkHub
	governor.Governor
}

// NewArk creates a new unified Ark instance by combining a resource Hub
// and a system Governor.
func NewArk(h hub.ArkHub, g governor.Governor) Ark {
	return &combinedArk{
		ArkHub:   h,
		Governor: g,
	}
}
