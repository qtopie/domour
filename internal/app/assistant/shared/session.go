package shared

import (
	"github.com/qtopie/domour/ark/storage"
)

// Session represents a conversation session, including its history and metadata.
type Session = storage.Session

// ProviderStat tracks per-provider usage metrics for a session.
type ProviderStat = storage.ProviderStat
