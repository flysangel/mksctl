package pkgindex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	code  int
	out   string
	err   error
	calls []call
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (int, []byte, error) {
	f.calls = append(f.calls, call{name, args})
	return f.code, []byte(f.out), f.err
}

func TestAptIndexHas(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{code: 0}
	idx := &aptIndex{r: f}
	ok, err := idx.Has(ctx, "curl")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, call{"apt-cache", []string{"show", "curl"}}, f.calls[0])

	f.code = 100
	ok, err = idx.Has(ctx, "nope")
	require.NoError(t, err)
	assert.False(t, ok)

	f.code = 2
	_, err = idx.Has(ctx, "x")
	assert.Error(t, err)

	f.err = errors.New("boom")
	_, err = idx.Has(ctx, "x")
	assert.Error(t, err)
}

func TestAptRefresh(t *testing.T) {
	f := &fakeRunner{}
	require.NoError(t, (&aptIndex{r: f}).Refresh(context.Background()))
	assert.Equal(t, call{"apt-get", []string{"update", "-o", "DPkg::Lock::Timeout=300"}}, f.calls[0])

	f = &fakeRunner{code: 100, out: strings.Repeat("x\n", 1000) + "last-line"}
	err := (&aptIndex{r: f}).Refresh(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "last-line")
	assert.Less(t, len(err.Error()), 2500)
}

func TestApkIndex(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{out: "curl\n"}
	idx := &apkIndex{r: f}
	ok, err := idx.Has(ctx, "curl")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, call{"apk", []string{"search", "-x", "curl"}}, f.calls[0])

	f.out = "\n"
	ok, err = idx.Has(ctx, "curl")
	require.NoError(t, err)
	assert.False(t, ok)

	f.code = 1
	_, err = idx.Has(ctx, "curl")
	assert.Error(t, err)

	f = &fakeRunner{}
	require.NoError(t, (&apkIndex{r: f}).Refresh(ctx))
	assert.Equal(t, call{"apk", []string{"update"}}, f.calls[0])
	f.code = 1
	f.out = "fail"
	assert.Error(t, (&apkIndex{r: f}).Refresh(ctx))
}

func TestFor(t *testing.T) {
	assert.IsType(t, &aptIndex{}, For(platform.Platform{Layers: []string{"ubuntu-24.04", "ubuntu", "apt"}}))
	assert.IsType(t, &apkIndex{}, For(platform.Platform{Layers: []string{"alpine", "apk"}}))
	idx := For(platform.Platform{Layers: []string{"weird"}})
	assert.Error(t, idx.Refresh(context.Background()))
	_, err := idx.Has(context.Background(), "x")
	assert.Error(t, err)
}

func TestExecRunner(t *testing.T) {
	r := ExecRunner{}
	code, out, err := r.Run(context.Background(), "sh", "-c", "echo hi; exit 3")
	require.NoError(t, err)
	assert.Equal(t, 3, code)
	assert.Equal(t, "hi\n", string(out))
	_, _, err = r.Run(context.Background(), "no-such-cmd-xyz")
	assert.Error(t, err)
}
