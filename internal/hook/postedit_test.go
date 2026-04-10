package hook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostEditHook_SourceFile(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(`{"file_path":"src/main.go"}`))
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "source file modified")
}

func TestPostEditHook_WikiFile(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(`{"file_path":".wiki/modules/auth.md"}`))
	require.NoError(t, err)
	assert.Empty(t, stderr.String(), "wiki files should not trigger reminder")
}

func TestPostEditHook_PlexiumFile(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(`{"file_path":".plexium/config.yml"}`))
	require.NoError(t, err)
	assert.Empty(t, stderr.String(), ".plexium files should not trigger reminder")
}

func TestPostEditHook_InvalidJSON(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(`not json`))
	require.NoError(t, err)
	assert.Empty(t, stderr.String(), "invalid JSON should silently succeed")
}

func TestPostEditHook_NestedParams(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(`{"params":{"file_path":"internal/foo.go"}}`))
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "source file modified")
}

func TestPostEditHook_EmptyInput(t *testing.T) {
	h := NewPostEditHook(t.TempDir())
	var stderr bytes.Buffer
	h.Stderr = &stderr

	err := h.Run(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, stderr.String())
}
