package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
)

func TestHealthBudgetFromEnv(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "CONTEXTMATRIX_SETUP_HEALTH_BUDGET" {
				return v
			}

			return ""
		}
	}

	var e engine.Engine

	require.NoError(t, healthFromEnv(&e, env("")))
	assert.False(t, e.SkipHealth)
	assert.Zero(t, e.HealthBudget, "unset keeps the default")

	require.NoError(t, healthFromEnv(&e, env("0")))
	assert.True(t, e.SkipHealth)

	e = engine.Engine{}
	require.NoError(t, healthFromEnv(&e, env("5s")))
	assert.False(t, e.SkipHealth)
	assert.Equal(t, 5*time.Second, e.HealthBudget)

	for _, bad := range []string{"soon", "-1s"} {
		require.Error(t, healthFromEnv(&engine.Engine{}, env(bad)), bad)
	}
}
