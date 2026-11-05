package agent

import "embed"

// Knowledge holds the guides that come with TitanOS (PLAN 49). They are
// written to the account's folder in the share KI-Agent, Wissen/TitanOS,
// and replaced on every update.
//
//go:embed knowledge/*.md
var Knowledge embed.FS
