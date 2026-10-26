package resolve

import (
	"testing"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderDedupAndNeedsFirst(t *testing.T) {
	c := recipe.Catalog{
		"base": {Name: "base"},
		"helm": {Name: "helm", Needs: []string{"base"}},
		"k":    {Name: "k", Needs: []string{"base", "helm"}},
	}
	got, err := Order(c, platform.Platform{Layers: []string{"common"}}, []string{"k", "helm"})
	require.NoError(t, err)
	assert.Equal(t, []string{"base", "helm", "k"}, got)
}

func TestOrderStepsKeepListOrder(t *testing.T) {
	c := recipe.Catalog{
		"base":    {Name: "base"},
		"ntp":     {Name: "ntp"},
		"netplan": {Name: "netplan"},
		"aconfig": {Name: "aconfig", Steps: map[string][]string{"ubuntu": {"ntp", "netplan"}}},
	}
	got, err := Order(c, platform.Platform{Layers: []string{"ubuntu-24.04", "ubuntu"}}, []string{"aconfig"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ntp", "netplan", "aconfig"}, got)
}

func TestOrderStepsNoMatchingLayer(t *testing.T) {
	c := recipe.Catalog{
		"ntp":     {Name: "ntp"},
		"aconfig": {Name: "aconfig", Steps: map[string][]string{"ubuntu": {"ntp"}}},
	}
	got, err := Order(c, platform.Platform{Layers: []string{"alpine-3.22", "apk"}}, []string{"aconfig"})
	require.NoError(t, err)
	assert.Equal(t, []string{"aconfig"}, got)
}

func TestOrderCycle(t *testing.T) {
	c := recipe.Catalog{
		"a": {Name: "a", Needs: []string{"b"}},
		"b": {Name: "b", Needs: []string{"a"}},
	}
	_, err := Order(c, platform.Platform{}, []string{"a"})
	assert.ErrorContains(t, err, "a -> b -> a")
}

func TestOrderSelfCycle(t *testing.T) {
	c := recipe.Catalog{"a": {Name: "a", Needs: []string{"a"}}}
	_, err := Order(c, platform.Platform{}, []string{"a"})
	assert.ErrorContains(t, err, "a -> a")
}

func TestOrderUnknownName(t *testing.T) {
	c := recipe.Catalog{"a": {Name: "a", Needs: []string{"ghost"}}}
	_, err := Order(c, platform.Platform{}, []string{"a"})
	assert.ErrorContains(t, err, "ghost")
	_, err = Order(c, platform.Platform{}, []string{"nope"})
	assert.ErrorContains(t, err, "nope")
}
