package opencode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveNamePrecedence(t *testing.T) {
	t.Setenv(EnvBin, "env-bin")

	require.Equal(t, "explicit-bin", resolveName("explicit-bin"))
	require.Equal(t, "env-bin", resolveName(""))
	require.Equal(t, "env-bin", resolveName("   "))

	t.Setenv(EnvBin, "")
	require.Equal(t, DefaultBin, resolveName(""))
}

func TestResolveBinUsesEnvOverride(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)

	t.Setenv(EnvBin, self)
	path, err := ResolveBin("")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(path), path)
}

func TestResolveBinMissing(t *testing.T) {
	t.Setenv(EnvBin, "")
	_, err := ResolveBin("definitely-not-a-real-binary-12345")
	require.ErrorIs(t, err, ErrNotFound)
}
