package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/charmbracelet/x/term"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
)

// unlockMCPProvider only installs an unlocked runtime. Network work belongs to
// explicit MCP tools, never to server startup or an automatic scheduler.
func unlockMCPProvider(ctx context.Context, opened OpenedProfile, streams IOStreams) error {
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		return err
	}
	if !connection.Bound || connection.Kind != "ynab" {
		return errors.New("--unlock requires a connected YNAB profile; connect it with moneyflow provider connect ynab first")
	}
	factory := streams.OpenYNAB
	if factory == nil {
		factory = defaultYNABCommandFactory
	}
	configured, err := factory(opened.Paths)
	if err != nil {
		return err
	}
	if configured.YNABVault == nil || configured.NewYNABSource == nil {
		return errors.New("YNAB vault runtime is unavailable")
	}
	exists, err := configured.YNABVault.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("YNAB vault is missing; reconnect with moneyflow provider connect ynab --profile <name-or-id>")
	}
	prompt := streams.Prompt
	if prompt == nil {
		prompt = controllingTerminalPrompt
	}
	password, err := prompt(ctx, "Moneyflow account password", true)
	if err != nil {
		return err
	}
	secret := []byte(password)
	defer clear(secret)
	credentials, err := configured.YNABVault.Load(secret)
	if err != nil {
		return fmt.Errorf("unlock YNAB vault: %w", err)
	}
	if credentials.PlanID != connection.RemoteProfileID {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	if credentials.Currency != connection.Currency || credentials.Scale != connection.Scale {
		return provider.NewError(provider.CodeMoneyMismatch)
	}
	readSource, writeSource, err := configured.NewYNABSource(credentials, nil)
	if err != nil {
		return err
	}
	if readSource == nil || writeSource == nil {
		return errors.New("YNAB provider source is unavailable")
	}
	instanceID, err := newProviderInstanceID("mcp")
	if err != nil {
		return err
	}
	return opened.Service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: readSource, WriteSource: writeSource, Provider: "ynab",
		Currency: connection.Currency, Scale: connection.Scale,
		Renderer: "mcp", InstanceID: instanceID, Now: configured.Now,
	})
}

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
