//go:build !windows

package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLockSecondWaiterTimesOut(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	unlock, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)
	defer unlock()

	_, err = Lock(context.Background(), p, 200*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "另一個 mksctl 正在執行")
	require.Contains(t, err.Error(), p+".lock")
}

func TestLockReleasedAllowsNext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	unlock, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)
	unlock()

	start := time.Now()
	unlock2, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)
	unlock2()
	require.Less(t, time.Since(start), 90*time.Millisecond)
}

func TestLockCanceledContext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	unlock, err := Lock(context.Background(), p, time.Second)
	require.NoError(t, err)
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Lock(ctx, p, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}
