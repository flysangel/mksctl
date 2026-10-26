//go:build windows

package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// LockFileEx 的鎖綁在 handle 上：同一個行程另開一個 handle 也搶不到，所以單一行程就能測互斥。
func TestLockExclusiveWindows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	unlock, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)

	_, err = Lock(context.Background(), p, 200*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "另一個 mksctl 正在執行")
	require.Contains(t, err.Error(), p+".lock")

	unlock()
	unlock2, err := Lock(context.Background(), p, 200*time.Millisecond)
	require.NoError(t, err)
	unlock2()
}

func TestLockCanceledContextWindows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	unlock, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Lock(ctx, p, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}
