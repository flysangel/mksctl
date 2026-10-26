package install

import (
	"testing"
	"testing/fstest"

	"github.com/flysangel/mksctl/internal/runner"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review Focus 3：沒裝過也要能 restore；配方拿不到 MKSCTL_STATE_IN，登錄值還原是冪等的。
func TestRestoreNeverInstalled(t *testing.T) {
	w := winDeps(t, "26100")
	diffs, err := Restore(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err)
	require.Empty(t, diffs)
	assert.Equal(t, []string{"run:rega:restore", "delete:A", "get:A"}, *w.Log)
	assert.NotContains(t, w.PS.envs["rega:restore"], "MKSCTL_STATE_IN")
	assert.NotContains(t, w.PS.envs["rega:restore"], "MKSCTL_STATE_OUT", "restore 不收配方資料")

	_, err = Restore(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err, "再還原一次也成功")
}

func TestRestoreDeletesStateEntry(t *testing.T) {
	w := winDeps(t, "26100")
	_, err := Run(ctx, w.Deps, Options{Names: []string{"rega", "regonly"}})
	require.NoError(t, err)
	require.Contains(t, w.machine(t).Recipes, "rega")

	_, err = Restore(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err)
	m := w.machine(t)
	assert.NotContains(t, m.Recipes, "rega")
	assert.Contains(t, m.Recipes, "regonly", "沒指名的配方不動")
	_, _, ok, _ := w.Store.winregMem.Get(`HKLM\Software\Mk`, "A")
	assert.False(t, ok, "restore: delete 的值要刪掉")
}

// 只有登錄值的配方還原時寫回 restore 宣告的值，不經 runner。
func TestRestoreRegistryValue(t *testing.T) {
	w := winDeps(t, "26100")
	_, err := Run(ctx, w.Deps, Options{Names: []string{"regonly"}})
	require.NoError(t, err)
	_, err = Restore(ctx, w.Deps, Options{Names: []string{"regonly"}})
	require.NoError(t, err)
	_, v, ok, _ := w.Store.winregMem.Get(`HKLM\Software\Mk`, "R")
	assert.True(t, ok)
	assert.Equal(t, "y", v)
	assert.Empty(t, w.Runner.calls)
}

func TestStateDataRoundTrip(t *testing.T) {
	w := winDeps(t, "26100")
	w.PS.writeOut["psonly"] = `{"created":["bob"]}`
	_, err := Run(ctx, w.Deps, Options{Names: []string{"psonly"}})
	require.NoError(t, err)
	assert.NotContains(t, w.PS.envs["psonly:install"], "MKSCTL_STATE_IN")
	assert.NotEmpty(t, w.PS.envs["psonly:install"]["MKSCTL_STATE_OUT"])
	assert.JSONEq(t, `{"created":["bob"]}`, string(w.machine(t).Recipes["psonly"].Data))

	// 第二次：拿到上次的資料；配方沒寫 OUT，資料保留。
	delete(w.PS.writeOut, "psonly")
	_, err = Run(ctx, w.Deps, Options{Names: []string{"psonly"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"created":["bob"]}`, w.PS.stateIn["psonly:install"])
	assert.JSONEq(t, `{"created":["bob"]}`, string(w.machine(t).Recipes["psonly"].Data))

	_, err = Restore(ctx, w.Deps, Options{Names: []string{"psonly"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"created":["bob"]}`, w.PS.stateIn["psonly:restore"])
	assert.NotContains(t, w.machine(t).Recipes, "psonly")
}

// 反序執行，而且只處理指名的配方，不展開 needs。
func TestRestoreReverseOrder(t *testing.T) {
	w := winDeps(t, "26100")
	_, err := Restore(ctx, w.Deps, Options{Names: []string{"wa", "wb"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"run:wb:restore", "run:wa:restore"}, *w.Log)

	*w.Log = nil
	_, err = Restore(ctx, w.Deps, Options{Names: []string{"wb"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"run:wb:restore"}, *w.Log)
}

// restore 前的檢查與 install 相同：不在對照表的 build 什麼都不動。
func TestRestoreUnknownBuildTouchesNothing(t *testing.T) {
	w := winDeps(t, "27000")
	diffs, err := Restore(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"/no-exact-platform"}, kinds(diffs))
	assert.Empty(t, *w.Log)
}

func TestRestoreScriptFailureKeepsState(t *testing.T) {
	w := winDeps(t, "26100")
	_, err := Run(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.NoError(t, err)
	w.PS.fail["rega"] = true
	_, err = Restore(ctx, w.Deps, Options{Names: []string{"rega"}})
	require.Error(t, err)
	assert.Contains(t, w.machine(t).Recipes, "rega", "腳本失敗時登錄值還沒還原，紀錄要留著")
}

// Linux 配方沒有 <名稱>_restore 時，在執行任何配方之前回錯誤。
func TestRestoreLinuxWithoutRestoreFunc(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	mfs := fx.Deps.FS.(fstest.MapFS)
	mfs["recipes/a/recipe.sh"] = &fstest.MapFile{Data: []byte("a_install() { :; }\na_verify() { :; }\na_restore() { :; }\n")}
	mfs["recipes/b/recipe.sh"] = &fstest.MapFile{Data: []byte("b_install() { :; }\nb_verify() { :; }\n")}
	_, err := Restore(ctx, fx.Deps, Options{Names: []string{"a", "b"}})
	require.ErrorContains(t, err, "配方 b 不支援 restore")
	assert.Empty(t, fx.Runner.calls)

	_, err = Restore(ctx, fx.Deps, Options{Names: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, fx.Runner.calls)
	assert.Equal(t, []runner.Action{runner.ActRestore}, fx.Runner.acts)
}

// 有版本的 Linux 配方即使已是最新版也要執行 restore，不能被 Decide 略過。
func TestRestoreVersionedRecipeNotSkipped(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	mfs := fx.Deps.FS.(fstest.MapFS)
	mfs["recipes/a/recipe.sh"] = &fstest.MapFile{Data: []byte("a_install() { :; }\na_verify() { :; }\na_restore() { :; }\n")}
	require.NoError(t, state.File{Recipes: map[string]state.Entry{"a": {Version: "v1"}}}.Save(fx.statePath()))
	_, err := Restore(ctx, fx.Deps, Options{Names: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, fx.Runner.calls)
	assert.NotContains(t, fx.state(t).Recipes, "a")
}
