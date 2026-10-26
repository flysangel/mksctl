package preflight

import (
	"context"
	"errors"
	"testing"

	"github.com/flysangel/mksctl/internal/pkgmap"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
)

func exactPlatform() platform.Platform {
	return platform.Platform{
		ID: "ubuntu", Version: "24.04",
		Layers:        []string{"ubuntu-24.04", "ubuntu", "apt", "common"},
		HasExactLayer: true,
		Provides:      map[string]bool{"systemd": true},
	}
}

func TestStaticClean(t *testing.T) {
	in := StaticInput{
		Platform: exactPlatform(),
		Recipes:  []recipe.Recipe{{Name: "a", Requires: []string{"systemd"}, Params: []string{"USER"}}},
		Params:   map[string]string{"USER": "bob"},
	}
	assert.Empty(t, Static(in))
}

func TestStaticNoExactPlatformReportedOnce(t *testing.T) {
	p := exactPlatform()
	p.Layers[0] = "ubuntu-28.04"
	p.HasExactLayer = false
	diffs := Static(StaticInput{Platform: p, Recipes: []recipe.Recipe{{Name: "a"}, {Name: "b"}}})
	assert.Equal(t, []Difference{{Recipe: "", Kind: NoExactPlatform, Detail: "ubuntu-28.04"}}, diffs)
}

func TestStaticMissingCapability(t *testing.T) {
	diffs := Static(StaticInput{
		Platform: exactPlatform(),
		Recipes:  []recipe.Recipe{{Name: "docker", Requires: []string{"systemd", "kernel"}}},
	})
	assert.Equal(t, []Difference{{Recipe: "docker", Kind: MissingCapability, Detail: "kernel"}}, diffs)
}

func TestStaticUnknownParam(t *testing.T) {
	diffs := Static(StaticInput{
		Platform: exactPlatform(),
		Recipes:  []recipe.Recipe{{Name: "a", Params: []string{"USER"}}, {Name: "b"}},
		Params:   map[string]string{"USER": "x", "TYPO": "y"},
	})
	assert.Equal(t, []Difference{{Recipe: "", Kind: UnknownParam, Detail: "TYPO"}}, diffs)
}

func TestStaticUnverified(t *testing.T) {
	in := StaticInput{Platform: exactPlatform(), Recipes: []recipe.Recipe{{Name: "u", Unverified: true}}}
	diffs := Static(in)
	if assert.Len(t, diffs, 1) {
		assert.Equal(t, Unverified, diffs[0].Kind)
		assert.Equal(t, "u", diffs[0].Recipe)
	}
	in.AllowUnverified = true
	assert.Empty(t, Static(in))
}

func TestStaticSortedByRecipeThenKind(t *testing.T) {
	p := exactPlatform()
	p.HasExactLayer = false
	diffs := Static(StaticInput{
		Platform: p,
		Recipes: []recipe.Recipe{
			{Name: "z", Unverified: true, Requires: []string{"kernel"}},
			{Name: "a", Requires: []string{"kernel"}},
		},
	})
	var got []string
	for _, d := range diffs {
		got = append(got, d.Recipe+"/"+string(d.Kind))
	}
	assert.Equal(t, []string{"/no-exact-platform", "a/missing-capability", "z/missing-capability", "z/unverified"}, got)
}

type fakeIndex struct {
	has map[string]bool
	err map[string]error
}

func (f fakeIndex) Refresh(context.Context) error { return nil }
func (f fakeIndex) Has(_ context.Context, pkg string) (bool, error) {
	return f.has[pkg], f.err[pkg]
}

func TestPackages(t *testing.T) {
	m, err := pkgmap.Load([]byte("dnsutils:\n  apk: [bind-tools]\nnope:\n  apt: null\n"))
	assert.NoError(t, err)
	idx := fakeIndex{
		has: map[string]bool{"curl": true, "dnsutils": true},
		err: map[string]error{"broken": errors.New("boom")},
	}
	p := exactPlatform()
	diffs := Packages(context.Background(), idx, m, p, map[string][]string{
		"b": {"curl", "missing"},
		"a": {"dnsutils", "nope", "broken"},
	})
	assert.Equal(t, []Difference{
		{Recipe: "a", Kind: PackageNotFound, Detail: "nope (apt 沒有這個套件)"},
		{Recipe: "a", Kind: PackageNotFound, Detail: "broken (查詢失敗：boom)"},
		{Recipe: "b", Kind: PackageNotFound, Detail: "missing"},
	}, diffs)
}
