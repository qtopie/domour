package session

import "github.com/qtopie/domour/internal/app/assistant/shared"

// Session is the conversation session type. It is defined in shared to avoid
// import cycles between bionic/session and infra/storage.
type Session = shared.Session

// ProviderStat tracks per-provider usage metrics.
type ProviderStat = shared.ProviderStat
