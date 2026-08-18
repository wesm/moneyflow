// Package paritydata embeds the canonical synthetic corpus for the portable demo command.
package paritydata

import "embed"

// Transactions is the canonical synthetic transaction fixture.
//
//go:embed transactions.json
var Transactions []byte

// Onboarding contains the strict Python selector and credential semantic contract.
//
//go:embed onboarding_scenarios.json onboarding_semantic_frames/*.json
var Onboarding embed.FS
