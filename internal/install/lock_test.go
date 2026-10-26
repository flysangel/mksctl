//go:build !windows

package install

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/stretchr/testify/assert"
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

// 整體 Run 的鎖路徑。
func lockedFixture(t *testing.T) (*fixture, func()) {
	t.Helper()
	fx := fakeDeps(t, ubuntu2404)
	fx.Deps.LockWait = 150 * time.Millisecond
	held, err := state.Lock(context.Background(), fx.statePath(), time.Second)
	require.NoError(t, err)
	return fx, held
}

func TestRunFailsWhenStateLocked(t *testing.T) {
	fx, held := lockedFixture(t)
	defer held()
	before := sysrootFiles(t, fx.Sysroot)
	_, err := Run(ctx, fx.Deps, Options{Names: []string{"docker", "helm"}})
	require.ErrorContains(t, err, "另一個 mksctl")
	assert.NoFileExists(t, fx.statePath())
	assert.Equal(t, before, sysrootFiles(t, fx.Sysroot), "沒取到鎖就不能寫任何來源")
	assert.Zero(t, fx.Index.RefreshCalls())
	assert.Empty(t, fx.Runner.calls)
}

func TestCheckOnlyIgnoresStateLock(t *testing.T) {
	fx, held := lockedFixture(t)
	defer held()
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}, CheckOnly: true})
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

// lockProbeRunner 在配方執行中另試一次取鎖，證明執行與存狀態檔期間鎖仍持有。
type lockProbeRunner struct {
	*fakeRunner
	statePath string
	probeErr  error
}

func (r *lockProbeRunner) Run(ctx context.Context, p platform.Platform, rc recipe.Recipe, env map[string]string) error {
	if u, err := state.Lock(ctx, r.statePath, 100*time.Millisecond); err != nil {
		r.probeErr = err
	} else {
		u()
	}
	return r.fakeRunner.Run(ctx, p, rc, env)
}

func TestLockHeldWhileExecuting(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	probe := &lockProbeRunner{fakeRunner: fx.Runner, statePath: fx.statePath()}
	fx.Deps.Runner = probe
	_, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	require.ErrorContains(t, probe.probeErr, "另一個 mksctl")
	assert.FileExists(t, fx.statePath())

	// Run 結束後鎖已放。
	u, err := state.Lock(context.Background(), fx.statePath(), 150*time.Millisecond)
	require.NoError(t, err)
	u()
}
