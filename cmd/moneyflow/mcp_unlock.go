package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/charmbracelet/x/term"
	"github.com/wesm/moneyflow/internal/assembly"
)

// Neither prompt output nor password input may use the MCP protocol streams.
func controllingTerminalPrompt(ctx context.Context, label string, _ bool) (value string, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	inputPath, outputPath := "/dev/tty", "/dev/tty"
	if runtime.GOOS == "windows" {
		inputPath, outputPath = "CONIN$", "CONOUT$"
	}
	input, err := os.OpenFile(inputPath, os.O_RDWR, 0)
	if err != nil {
		return "", errors.New("--unlock requires a controlling terminal; start moneyflow mcp --unlock --transport streamable-http from a terminal, or omit --unlock for offline access")
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	if !term.IsTerminal(input.Fd()) {
		return "", errors.New("--unlock requires an interactive controlling terminal")
	}
	output, err := os.OpenFile(outputPath, os.O_WRONLY, 0)
	if err != nil {
		return "", fmt.Errorf("open controlling terminal output: %w", err)
	}
	defer func() { err = errors.Join(err, output.Close()) }()
	if _, err = fmt.Fprintf(output, "%s: ", label); err != nil {
		return "", err
	}
	return readMaskedSecret(input, bufio.NewReader(input), output)
}

// unlockMCPProvider only installs an unlocked runtime. Network work belongs to
// explicit MCP tools, never to server startup or an automatic scheduler.
func unlockMCPProvider(ctx context.Context, opened OpenedProfile, streams IOStreams) error {
	prompt := streams.Prompt
	if prompt == nil {
		prompt = controllingTerminalPrompt
	}
	err := assembly.UnlockYNAB(ctx, assembly.UnlockRequest{
		Service: opened.Service, Paths: opened.Paths, YNAB: streams.OpenYNAB, Renderer: "mcp",
		Password: func(promptContext context.Context) (string, error) {
			return prompt(promptContext, "Moneyflow account password", true)
		},
	})
	switch {
	case errors.Is(err, assembly.ErrNotYNAB):
		return errors.New("--unlock requires a connected YNAB profile; " +
			"connect it with moneyflow provider connect ynab first")
	case errors.Is(err, assembly.ErrVaultMissing):
		return errors.New("YNAB vault is missing; " +
			"reconnect with moneyflow provider connect ynab --profile <name-or-id>")
	default:
		return err
	}
}
