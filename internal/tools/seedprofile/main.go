// Command seedprofile creates one explicit synthetic SQLite profile for integration tests.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "seed explicit test profile:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return errors.New("exactly one explicit profile root is required")
	}
	paths, err := home.ResolveRoot(args[0], nil, "")
	if err != nil {
		return errors.New("resolve explicit profile root")
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		return errors.New("open explicit profile")
	}
	defer func() { _ = profile.Close() }()
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	if err != nil {
		return errors.New("decode synthetic fixture")
	}
	committed, err := fixture.CommittedProfile(transactions)
	if err != nil {
		return errors.New("normalize synthetic fixture")
	}
	if _, err = profile.CreateSeededProfile(ctx, committed); err != nil {
		return errors.New("create seeded profile")
	}
	return nil
}
