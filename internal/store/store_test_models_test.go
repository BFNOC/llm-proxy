package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test Models CRUD
// ---------------------------------------------------------------------------

func TestTestModel_CreateAndList(t *testing.T) {
	s := newTestStore(t)

	m1, err := s.CreateTestModel("gpt-4o", "openai")
	require.NoError(t, err)
	require.NotNil(t, m1)
	assert.Positive(t, m1.ID)
	assert.Equal(t, "gpt-4o", m1.Name)
	assert.Equal(t, "openai", m1.Protocol)
	assert.False(t, m1.CreatedAt.IsZero())

	m2, err := s.CreateTestModel("claude-sonnet-4-20250514", "anthropic")
	require.NoError(t, err)
	require.NotNil(t, m2)

	// List all
	all, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestTestModel_ListByProtocol(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateTestModel("gpt-4o", "openai")
	require.NoError(t, err)
	_, err = s.CreateTestModel("gpt-4o-mini", "openai")
	require.NoError(t, err)
	_, err = s.CreateTestModel("claude-sonnet-4-20250514", "anthropic")
	require.NoError(t, err)

	openaiModels, err := s.ListTestModels("openai")
	require.NoError(t, err)
	assert.Len(t, openaiModels, 2)
	for _, m := range openaiModels {
		assert.Equal(t, "openai", m.Protocol)
	}

	anthropicModels, err := s.ListTestModels("anthropic")
	require.NoError(t, err)
	assert.Len(t, anthropicModels, 1)
	assert.Equal(t, "claude-sonnet-4-20250514", anthropicModels[0].Name)
}

func TestTestModel_ListEmpty(t *testing.T) {
	s := newTestStore(t)
	models, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestTestModel_Update(t *testing.T) {
	s := newTestStore(t)

	m, err := s.CreateTestModel("old-model", "openai")
	require.NoError(t, err)

	err = s.UpdateTestModel(m.ID, "new-model", "anthropic")
	require.NoError(t, err)

	// Verify the update
	models, err := s.ListTestModels("")
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "new-model", models[0].Name)
	assert.Equal(t, "anthropic", models[0].Protocol)
}

func TestTestModel_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateTestModel(9999, "name", "openai")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTestModel_Delete(t *testing.T) {
	s := newTestStore(t)

	m, err := s.CreateTestModel("to-delete", "openai")
	require.NoError(t, err)

	err = s.DeleteTestModel(m.ID)
	require.NoError(t, err)

	models, err := s.ListTestModels("")
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestTestModel_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteTestModel(9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
