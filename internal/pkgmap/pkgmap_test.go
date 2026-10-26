package pkgmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupVersionLayerWins(t *testing.T) {
	m, err := Load([]byte("ntpdate:\n  apt: [ntpdate]\n  ubuntu-26.04: [ntpsec-ntpdate]\n  apk: null\n"))
	require.NoError(t, err)
	got, err := m.Lookup("ntpdate", []string{"ubuntu-26.04", "ubuntu", "apt", "common"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ntpsec-ntpdate"}, got)
	got, err = m.Lookup("ntpdate", []string{"ubuntu-24.04", "ubuntu", "apt", "common"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ntpdate"}, got)
	_, err = m.Lookup("ntpdate", []string{"alpine-3.22", "apk", "common"})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestLookupFallbacks(t *testing.T) {
	m, err := Load([]byte("x:\n  apt: [y]\n"))
	require.NoError(t, err)
	got, _ := m.Lookup("absent", []string{"apt"})
	assert.Equal(t, []string{"absent"}, got)
	got, _ = m.Lookup("x", []string{"apk", "common"})
	assert.Equal(t, []string{"x"}, got)
}
