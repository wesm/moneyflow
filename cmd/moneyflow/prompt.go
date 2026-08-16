package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

func terminalPrompt(command *cobra.Command) PromptFunc {
	reader := bufio.NewReader(command.InOrStdin())
	return func(_ context.Context, label string, secret bool) (string, error) {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s: ", label); err != nil {
			return "", err
		}
		if !secret {
			value, err := reader.ReadString('\n')
			return strings.TrimRight(value, "\r\n"), err
		}
		input, ok := command.InOrStdin().(*os.File)
		if !ok || !term.IsTerminal(input.Fd()) {
			return "", errors.New("secret prompt requires an interactive terminal")
		}
		return readMaskedSecret(input, reader, command.ErrOrStderr())
	}
}

func readMaskedSecret(
	input *os.File,
	reader *bufio.Reader,
	output io.Writer,
) (string, error) {
	state, err := term.MakeRaw(input.Fd())
	if err != nil {
		return "", fmt.Errorf("prepare secret prompt: %w", err)
	}
	secret, readErr := readMaskedInput(reader, output)
	restoreErr := term.Restore(input.Fd(), state)
	if restoreErr != nil {
		restoreErr = fmt.Errorf("restore terminal after secret prompt: %w", restoreErr)
	}
	return secret, errors.Join(readErr, restoreErr)
}

func readMaskedInput(input io.Reader, output io.Writer) (string, error) {
	reader, ok := input.(interface {
		ReadRune() (rune, int, error)
	})
	if !ok {
		reader = bufio.NewReader(input)
	}
	secret := make([]rune, 0, 32)
	clearLast := func() error {
		if _, err := io.WriteString(output, "\b \b"); err != nil {
			return err
		}
		secret = secret[:len(secret)-1]
		return nil
	}
	for {
		character, _, err := reader.ReadRune()
		if err != nil {
			return "", err
		}
		switch character {
		case '\r', '\n':
			if _, err = io.WriteString(output, "\r\n"); err != nil {
				return "", err
			}
			return string(secret), nil
		case 0x03:
			_, writeErr := io.WriteString(output, "^C\r\n")
			return "", errors.Join(context.Canceled, writeErr)
		case '\b', 0x7f:
			if len(secret) > 0 {
				if err = clearLast(); err != nil {
					return "", err
				}
			}
		case 0x15:
			for len(secret) > 0 {
				if err = clearLast(); err != nil {
					return "", err
				}
			}
		default:
			if unicode.IsControl(character) {
				continue
			}
			secret = append(secret, character)
			if _, err = io.WriteString(output, "*"); err != nil {
				return "", err
			}
		}
	}
}
