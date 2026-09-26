// Command moneyflow runs the portable moneyflow application.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCommand(IOStreams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
