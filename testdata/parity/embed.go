// Package paritydata embeds the synthetic transaction corpus shared by demos and logical tests.
package paritydata

import _ "embed"

// Transactions is the canonical synthetic transaction fixture.
//
//go:embed transactions.json
var Transactions []byte
