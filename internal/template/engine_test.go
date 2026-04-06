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

func TestModuleData(t *testing.T) {
	data := ModuleData{
		Title:         "Auth Module",
		LastUpdated:   "2024-01-01",
		UpdatedBy:     "agent-1",
		RelatedModules: []string{"UserModule", "SessionModule"},
		SourceFiles:   []string{"src/auth/*.go"},
		Confidence:    "high",
		ReviewStatus:  "unreviewed",
		Tags:          []string{"auth", "security"},
		Body:          "Authentication module content",
	}

	assert.Equal(t, "Auth Module", data.Title)
	assert.Len(t, data.RelatedModules, 2)
	assert.Contains(t, data.Tags, "auth")
}
