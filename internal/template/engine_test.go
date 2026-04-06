package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_Register(t *testing.T) {
	e, err := New("")
	require.NoError(t, err)

	err = e.Register("test", `Hello {{.Name}}`)
	require.NoError(t, err)

	result, err := e.Render("test", map[string]any{"Name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello World", result)
}

func TestEngine_Render_NotFound(t *testing.T) {
	e, err := New("")
	require.NoError(t, err)

	_, err = e.Render("nonexistent", nil)
	assert.Error(t, err)
}

func TestDefaultEngine(t *testing.T) {
	e, err := DefaultEngine()
	require.NoError(t, err)
	assert.NotNil(t, e)
}
