// Package paritydata embeds the canonical synthetic corpus for the portable demo command.
package paritydata

import _ "embed"

// Transactions is the canonical synthetic transaction fixture.
//
//go:embed transactions.json
var Transactions []byte
