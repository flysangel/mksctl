//go:build windows

package winreg

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/registry"
)

// 在 HKCU\Software\mksctl-test\<隨機> 上跑完整的 Apply、Verify、Restore，結束時刪掉測試鍵。
func TestSystemStoreRoundTrip(t *testing.T) {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	sub := `Software\mksctl-test\` + hex.EncodeToString(b)
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, sub)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\mksctl-test`)
	})
	key := `HKCU\` + sub
	s := NewSystemStore()
	vals := []Value{
		{Key: key, Name: "D", Type: "dword", Value: 0x30000, Restore: RestoreSpec{Mode: "delete"}},
		{Key: key, Name: "Q", Type: "qword", Value: 1 << 40, Restore: RestoreSpec{Mode: "value", Value: 2}},
		{Key: key, Name: "S", Type: "string", Value: "x", Restore: RestoreSpec{Mode: "keep"}},
		{Key: key, Name: "E", Type: "expand_string", Value: "%P%", Restore: RestoreSpec{Mode: "delete"}},
		{Key: key, Name: "M", Type: "multi_string", Value: []any{"a", "b"}, Restore: RestoreSpec{Mode: "delete"}},
		{Key: key + `\Missing`, Name: "N", Type: "dword", Value: 1, OnlyIfKeyExists: true, Restore: RestoreSpec{Mode: "delete"}},
	}
	_, err := Apply(s, vals)
	require.NoError(t, err)
	_, err = Verify(s, vals)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = Restore(s, vals)
		require.NoError(t, err)
		require.NoError(t, VerifyRestored(s, vals))
	}
	_, v, ok, err := s.Get(key, "Q")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint64(2), v)
	_, _, ok, _ = s.Get(key, "D")
	assert.False(t, ok)
}
