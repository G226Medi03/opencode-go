package opencode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	v, err := ParseVersion("v2.0.32\n")
	require.NoError(t, err)
	require.Equal(t, 2, v.Major)
	require.Equal(t, 0, v.Minor)
	require.Equal(t, 32, v.Patch)
	require.Equal(t, "2.0.32", v.String())
	require.True(t, v.IsV2())
}

func TestParseVersionRejectsGarbage(t *testing.T) {
	_, err := ParseVersion("no version here")
	require.Error(t, err)
}

func TestVersionV1IsUnsupported(t *testing.T) {
	v, err := ParseVersion("1.9.0")
	require.NoError(t, err)
	require.False(t, v.IsV2())
}
