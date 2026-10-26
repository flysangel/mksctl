//go:build !windows

package install

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/runner"
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

func (r *lockProbeRunner) Run(ctx context.Context, p platform.Platform, rc recipe.Recipe, env map[string]string, act runner.Action) error {
	if u, err := state.Lock(ctx, r.statePath, 100*time.Millisecond); err != nil {
		r.probeErr = err
	} else {
		u()
	}
	return r.fakeRunner.Run(ctx, p, rc, env, act)
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

// Linux 的 machine 與 user 狀態檔是同一個檔：同一個路徑只能鎖一次，否則自己卡死自己。
func TestLockStatesSamePathOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	unlock, err := lockStates(context.Background(), []string{p, p}, 150*time.Millisecond)
	require.NoError(t, err)
	unlock()
}

// Windows 的 machine 與 user 配方混用時，執行期間兩個狀態檔都鎖著；restore 也一樣。
func TestWindowsLocksBothStates(t *testing.T) {
	w := winDeps(t, "26100")
	var probeErrs []error
	w.PS.probe = func() {
		for _, p := range []string{w.Deps.Paths.MachineState, w.Deps.Paths.UserState} {
			u, err := state.Lock(context.Background(), p, 50*time.Millisecond)
			if err == nil {
				u()
			}
			probeErrs = append(probeErrs, err)
		}
	}
	_, err := Run(ctx, w.Deps, Options{Names: []string{"rega", "ureg"}})
	require.NoError(t, err)
	_, err = Restore(ctx, w.Deps, Options{Names: []string{"rega", "ureg"}})
	require.NoError(t, err)
	require.Len(t, probeErrs, 8)
	for _, e := range probeErrs {
		assert.ErrorContains(t, e, "另一個 mksctl")
	}
}

// 只有 machine 配方時不碰使用者的狀態檔，連鎖檔都不建。
func TestWindowsMachineOnlyLeavesUserStateAlone(t *testing.T) {
	w := winDeps(t, "26100")
	_, err := Run(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Dir(w.Deps.Paths.UserState))
}
