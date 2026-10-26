package winreg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const k = `HKLM\SOFTWARE\X`

func dw(v any, r RestoreSpec) []Value {
	return []Value{{Key: k, Name: "A", Type: "dword", Value: v, Restore: r}}
}

func TestApplyThenVerify(t *testing.T) {
	s := NewMemStore()
	vals := dw(1, RestoreSpec{Mode: "delete"})
	_, err := Apply(s, vals)
	require.NoError(t, err)
	_, err = Verify(s, vals)
	assert.NoError(t, err)
	// 鍵名大小寫不分
	_, _, ok, _ := s.Get(`hklm\software\x`, "a")
	assert.True(t, ok)
}

func TestVerifyWrongValueFails(t *testing.T) {
	s := NewMemStore()
	require.NoError(t, s.Set(k, "A", "dword", uint32(0)))
	_, err := Verify(s, dw(1, RestoreSpec{Mode: "delete"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `HKLM\SOFTWARE\X\A 應為 1，實際為 0`)
}

func TestVerifyMissingAndWrongType(t *testing.T) {
	s := NewMemStore()
	_, err := Verify(s, dw(1, RestoreSpec{Mode: "delete"}))
	assert.Error(t, err)
	require.NoError(t, s.Set(k, "A", "string", "1"))
	_, err = Verify(s, dw(1, RestoreSpec{Mode: "delete"}))
	assert.Error(t, err)
}

func TestOnlyIfKeyExistsSkips(t *testing.T) {
	s := NewMemStore()
	vals := dw(1, RestoreSpec{Mode: "delete"})
	vals[0].OnlyIfKeyExists = true
	res, err := Apply(s, vals)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.True(t, res[0].Skipped)
	ok, _ := s.KeyExists(k)
	assert.False(t, ok)
	res, err = Verify(s, vals)
	require.NoError(t, err)
	assert.True(t, res[0].Skipped)
	res, err = Restore(s, vals)
	require.NoError(t, err)
	assert.True(t, res[0].Skipped)
}

func TestEnsureAbsent(t *testing.T) {
	s := NewMemStore()
	require.NoError(t, s.Set(k, "A", "dword", uint32(5)))
	vals := dw(nil, RestoreSpec{Mode: "delete"})
	vals[0].Ensure = "absent"
	_, err := Verify(s, vals)
	assert.Error(t, err)
	_, err = Apply(s, vals)
	require.NoError(t, err)
	_, err = Verify(s, vals)
	assert.NoError(t, err)
	// Restore 不動
	require.NoError(t, s.Set(k, "A", "dword", uint32(7)))
	_, err = Restore(s, vals)
	require.NoError(t, err)
	_, v, ok, _ := s.Get(k, "A")
	assert.True(t, ok)
	assert.Equal(t, uint32(7), v)
}

func TestRestoreModes(t *testing.T) {
	s := NewMemStore()
	vals := []Value{
		{Key: k, Name: "D", Type: "dword", Value: 1, Restore: RestoreSpec{Mode: "delete"}},
		{Key: k, Name: "V", Type: "dword", Value: 1, Restore: RestoreSpec{Mode: "value", Value: 9}},
		{Key: k, Name: "K", Type: "dword", Value: 1, Restore: RestoreSpec{Mode: "keep"}},
	}
	_, err := Apply(s, vals)
	require.NoError(t, err)
	for i := 0; i < 2; i++ { // 重跑仍成功
		_, err = Restore(s, vals)
		require.NoError(t, err)
		require.NoError(t, VerifyRestored(s, vals))
		_, _, ok, _ := s.Get(k, "D")
		assert.False(t, ok)
		_, v, _, _ := s.Get(k, "V")
		assert.Equal(t, uint32(9), v)
		_, v, _, _ = s.Get(k, "K")
		assert.Equal(t, uint32(1), v)
	}
	require.NoError(t, s.Set(k, "V", "dword", uint32(1)))
	assert.Error(t, VerifyRestored(s, vals))
}

func TestHexDword(t *testing.T) {
	var vals []Value
	require.NoError(t, yaml.Unmarshal([]byte("- {key: 'HKLM\\SOFTWARE\\X', name: A, type: dword, value: 0x00030000, restore: delete}\n"), &vals))
	s := NewMemStore()
	_, err := Apply(s, vals)
	require.NoError(t, err)
	_, v, _, _ := s.Get(k, "A")
	assert.Equal(t, uint32(196608), v)
	_, err = Verify(s, vals)
	assert.NoError(t, err)
}

func TestConversion(t *testing.T) {
	s := NewMemStore()
	vals := []Value{
		{Key: k, Name: "Q", Type: "qword", Value: 1 << 40, Restore: RestoreSpec{Mode: "delete"}},
		{Key: k, Name: "M", Type: "multi_string", Value: []any{"a", "b"}, Restore: RestoreSpec{Mode: "delete"}},
		{Key: k, Name: "S", Type: "string", Value: "x", Restore: RestoreSpec{Mode: "delete"}},
		{Key: k, Name: "E", Type: "expand_string", Value: "%P%", Restore: RestoreSpec{Mode: "delete"}},
	}
	_, err := Apply(s, vals)
	require.NoError(t, err)
	_, err = Verify(s, vals)
	require.NoError(t, err)
	_, v, _, _ := s.Get(k, "M")
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestConversionErrors(t *testing.T) {
	for _, bad := range []Value{
		{Key: k, Name: "A", Type: "dword", Value: -1},
		{Key: k, Name: "A", Type: "dword", Value: 1 << 33},
		{Key: k, Name: "A", Type: "qword", Value: -1},
		{Key: k, Name: "A", Type: "dword", Value: "x"},
		{Key: k, Name: "A", Type: "multi_string", Value: []any{1}},
		{Key: k, Name: "A", Type: "nope", Value: 1},
	} {
		bad.Restore = RestoreSpec{Mode: "delete"}
		_, err := Apply(NewMemStore(), []Value{bad})
		assert.Error(t, err, "%+v", bad)
	}
}

func TestBadRoot(t *testing.T) {
	_, err := Apply(NewMemStore(), []Value{{Key: `HKU\X`, Name: "A", Type: "dword", Value: 1, Restore: RestoreSpec{Mode: "delete"}}})
	assert.Error(t, err)
	_, err = Apply(NewMemStore(), []Value{{Key: `HKCR\X`, Name: "A", Type: "dword", Value: 1, Restore: RestoreSpec{Mode: "delete"}}})
	assert.NoError(t, err)
}

func TestRestoreUnmarshal(t *testing.T) {
	var v struct {
		R RestoreSpec `yaml:"restore"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("restore: delete\n"), &v))
	assert.Equal(t, RestoreSpec{Mode: "delete"}, v.R)
	require.NoError(t, yaml.Unmarshal([]byte("restore: keep\n"), &v))
	assert.Equal(t, RestoreSpec{Mode: "keep"}, v.R)
	require.NoError(t, yaml.Unmarshal([]byte("restore: {value: 2}\n"), &v))
	assert.Equal(t, RestoreSpec{Mode: "value", Value: 2}, v.R)
	assert.Error(t, yaml.Unmarshal([]byte("restore: bogus\n"), &v))
	assert.Error(t, yaml.Unmarshal([]byte("restore: {other: 1}\n"), &v))
}
