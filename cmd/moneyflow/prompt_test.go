package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMaskedInputShowsFeedbackAndSupportsEditing(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	secret, err := readMaskedInput(strings.NewReader("pássx\bword\r"), &output)
	require.NoError(t, err)
	assert.Equal(t, "pássword", secret)
	assert.Equal(t, "*****\b \b****\r\n", output.String())
	assert.NotContains(t, output.String(), secret)
}

func TestReadMaskedInputClearsTheLineWithoutLeakingIt(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	secret, err := readMaskedInput(strings.NewReader("discard\x15kept\n"), &output)
	require.NoError(t, err)
	assert.Equal(t, "kept", secret)
	assert.Equal(t, "*******\b \b\b \b\b \b\b \b\b \b\b \b\b \b****\r\n", output.String())
	assert.NotContains(t, output.String(), "discard")
}

func TestReadMaskedInputCancelsOnControlC(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	secret, err := readMaskedInput(strings.NewReader("secret\x03"), &output)
	assert.Empty(t, secret)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "******^C\r\n", output.String())
	assert.NotContains(t, output.String(), "secret")
}
