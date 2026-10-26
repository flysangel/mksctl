//go:build !windows

package install

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/flysangel/mksctl/internal/state"
	"github.com/stretchr/testify/require"
)

// 第二個狀態檔被占用時，第一個已取到的鎖要放掉，否則會留下卡死的鎖。
func TestLockStatesReleasesOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	held, err := state.Lock(context.Background(), b, time.Second)
	require.NoError(t, err)

	_, err = lockStates(context.Background(), []string{a, b}, 150*time.Millisecond)
	require.ErrorContains(t, err, "另一個 mksctl 正在執行")

	// a 的鎖已放，立刻可取。
	u, err := state.Lock(context.Background(), a, 150*time.Millisecond)
	require.NoError(t, err)
	u()
	held()

	unlock, err := lockStates(context.Background(), []string{a, b}, time.Second)
	require.NoError(t, err)
	unlock()
}
