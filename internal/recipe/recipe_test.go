package recipe

import (
	"os"
	"testing"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateUnknownCapability(t *testing.T) {
	c := Catalog{"docker": {Name: "docker", Requires: []string{"systemD"}, Venue: "lxc"}}
	defs := platform.Defs{"apt": {Provides: []string{"systemd"}}}
	errs := Validate(c, defs)
	assert.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "systemD")
}

func TestValidateArtifactNeedsHashSource(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "podman", Artifacts: []Artifact{{ID: "bin", URL: "https://e/x"}}}}
	errs := Validate(c, platform.Defs{})
	assert.ErrorContains(t, errs[0], "雜湊來源")
}

func TestValidateUnverifiedSkipsHashSource(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", Unverified: true, Artifacts: []Artifact{{ID: "bin"}}}}
	assert.Empty(t, Validate(c, platform.Defs{}))
}

func TestValidateCollectsAllErrors(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "bad", Needs: []string{"nope"},
		Steps: map[string][]string{"ghost": {"nope2"}}, Latest: &LatestSpec{}}}
	errs := Validate(c, platform.Defs{})
	assert.Len(t, errs, 5) // needs、steps 層、steps 成員、venue、latest
}

func TestValidateLatestExactlyOne(t *testing.T) {
	bad := []LatestSpec{
		{GitHub: "a/b", URL: "u"},
		{GitHub: "a/b", Provider: "godev"},
		{URL: "u", Provider: "godev"},
		{GitHub: "a/b", URL: "u", Provider: "godev"},
	}
	for _, l := range bad {
		c := Catalog{"x": {Name: "x", Venue: "vm", Latest: &l}}
		errs := Validate(c, platform.Defs{})
		require.Len(t, errs, 1, "%+v", l)
		assert.ErrorContains(t, errs[0], "擇一")
	}
	for _, l := range []LatestSpec{{GitHub: "a/b"}, {URL: "u"}, {Provider: "godev"}} {
		c := Catalog{"x": {Name: "x", Venue: "vm", Latest: &l}}
		assert.Empty(t, Validate(c, platform.Defs{}), "%+v", l)
	}
}

func TestValidateUnknownProvider(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", Latest: &LatestSpec{Provider: "nope"},
		Artifacts: []Artifact{{ID: "bin", URL: "https://e/x", Checksum: ChecksumSpec{Provider: "nope2"}}}}}
	errs := Validate(c, platform.Defs{})
	require.Len(t, errs, 2)
	all := errs[0].Error() + "\n" + errs[1].Error()
	assert.Contains(t, all, `latest.provider "nope"`)
	assert.Contains(t, all, `checksum.provider "nope2"`)
}

func TestValidateProviderChecksumIsHashSource(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm",
		Artifacts: []Artifact{{ID: "bin", URL: "https://e/x", Checksum: ChecksumSpec{Provider: "godev"}}}}}
	assert.Empty(t, Validate(c, platform.Defs{}))
}

func TestValidateDuplicateAptFile(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", AptSources: []AptSource{
		{File: "x.sources", URIs: "https://a"},
		{File: "x.sources", URIs: "https://b"},
	}}}
	errs := Validate(c, platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "x.sources")
}

func TestLoadRejectsOldAptSourceKey(t *testing.T) {
	_, err := Load(os.DirFS("testdata/oldaptkey"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "apt_source")
}

func TestLoadStepsPerLayer(t *testing.T) {
	c, err := Load(os.DirFS("testdata/catalog"))
	require.NoError(t, err)
	assert.Equal(t, []string{"base", "netplan"}, c["aconfig"].Steps["ubuntu"])
	assert.Equal(t, []string{"base"}, c["aconfig"].Steps["apk"])
	assert.Equal(t, "aconfig", c["aconfig"].Name)
	assert.True(t, c["base"].Versioned)
	assert.False(t, c["netplan"].Versioned)
}

func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := Load(os.DirFS("testdata/badfield"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "oops")
}

func TestValidateRuntimeCapabilitiesAlwaysProvided(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", Requires: []string{"kernel", "init"}}}
	assert.Empty(t, Validate(c, platform.Defs{}))
}

func TestValidateSigstoreKeyOrIdentity(t *testing.T) {
	cat := func(s SigstoreSpec) Catalog {
		return Catalog{"x": {Name: "x", Venue: "vm", Artifacts: []Artifact{{ID: "bin", URL: "https://e/x",
			Checksum: ChecksumSpec{Sigstore: &s}}}}}
	}
	keyless := SigstoreSpec{Bundle: "b", Identity: "^https://e/", Issuer: "https://i"}
	keyed := SigstoreSpec{Bundle: "b", Key: "trust/keys/x.pub"}
	assert.Empty(t, Validate(cat(keyless), platform.Defs{}))
	assert.Empty(t, Validate(cat(keyed), platform.Defs{}))

	both := keyless
	both.Key = "trust/keys/x.pub"
	errs := Validate(cat(both), platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "key")

	errs = Validate(cat(SigstoreSpec{Bundle: "b"}), platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "key")

	// keyless 缺一半：只比 SAN 不比 issuer，任何 OIDC 提供者簽的都會過。
	errs = Validate(cat(SigstoreSpec{Bundle: "b", Identity: "^https://e/"}), platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "issuer")
}

func dw(key, name string, v int) RegValue {
	return RegValue{Key: key, Name: name, Type: "dword", Value: v, Restore: RegRestore{Mode: "delete"}}
}

func winDefs() platform.Defs {
	return platform.Defs{"windows": {Provides: []string{"windows", "registry", "powershell"}}}
}

func winRecipe(venue, scope string, regs ...RegValue) Catalog {
	return Catalog{"x": {Name: "x", Requires: []string{"windows"}, Venue: venue, Scope: scope, Registry: regs}}
}

func TestValidateWindowsVenue(t *testing.T) {
	errs := Validate(winRecipe("podman", "", dw(`HKLM\SOFTWARE\X`, "A", 1)), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "actions、vm")
	assert.Empty(t, Validate(winRecipe("actions", "", dw(`HKLM\SOFTWARE\X`, "A", 1)), winDefs()))
	assert.Empty(t, Validate(winRecipe("vm", "", dw(`HKLM\SOFTWARE\X`, "A", 1)), winDefs()))
}

func TestValidateLinuxVenueRejectsActions(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "actions"}}
	errs := Validate(c, platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "podman、lxc、vm")
}

func TestValidateScopeAndRegistryOnlyForWindows(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", Scope: "user"}, "y": {Name: "y", Venue: "vm", Registry: []RegValue{dw(`HKLM\X`, "A", 1)}}}
	assert.Len(t, Validate(c, platform.Defs{}), 2)
}

func TestValidateBadScope(t *testing.T) {
	errs := Validate(winRecipe("vm", "root", dw(`HKLM\X`, "A", 1)), winDefs())
	require.NotEmpty(t, errs)
	assert.ErrorContains(t, errs[0], "scope")
}

func TestValidateRegistryType(t *testing.T) {
	bad := dw(`HKLM\X`, "A", 1)
	bad.Type = "bool"
	errs := Validate(winRecipe("vm", "", bad), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "type")
}

func TestValidateAbsentRules(t *testing.T) {
	ok := RegValue{Key: `HKLM\X`, Name: "A", Ensure: "absent", Restore: RegRestore{Mode: "keep"}, Reason: "不還原"}
	assert.Empty(t, Validate(winRecipe("vm", "", ok), winDefs()))
	bad := ok
	bad.Type, bad.Value, bad.Restore = "dword", 1, RegRestore{Mode: "delete"}
	errs := Validate(winRecipe("vm", "", bad), winDefs())
	require.Len(t, errs, 2)
	assert.ErrorContains(t, errs[0], "type 或 value")
	assert.ErrorContains(t, errs[1], "keep")
}

func TestValidateKeepNeedsReason(t *testing.T) {
	v := dw(`HKLM\X`, "A", 1)
	v.Restore = RegRestore{Mode: "keep"}
	errs := Validate(winRecipe("vm", "", v), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "reason")
	v.Reason = "原因"
	assert.Empty(t, Validate(winRecipe("vm", "", v), winDefs()))
}

func TestValidateHKCUNeedsUserScope(t *testing.T) {
	errs := Validate(winRecipe("vm", "machine", dw(`HKCU\X`, "A", 1)), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "scope 必須是 user")
	errs = Validate(winRecipe("vm", "user", dw(`HKLM\X`, "A", 1)), winDefs())
	require.Len(t, errs, 1)
	assert.Empty(t, Validate(winRecipe("vm", "user", dw(`HKCU\X`, "A", 1)), winDefs()))
	assert.Empty(t, Validate(winRecipe("vm", "", dw(`HKCR\X`, "A", 1)), winDefs()))
	errs = Validate(winRecipe("vm", "", dw(`HKU\X`, "A", 1)), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "根")
}

func TestValidateWindowsNeedsRegistryWithoutPs1(t *testing.T) {
	errs := Validate(winRecipe("vm", ""), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "registry")
}

func TestValidatePs1NeedsRestore(t *testing.T) {
	c, err := Load(os.DirFS("testdata/win"))
	require.NoError(t, err)
	assert.True(t, c["x"].HasPs1)
	assert.False(t, c["x"].HasSh)
	errs := Validate(c, winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "x_restore")
}

func TestLoadRegRestoreForms(t *testing.T) {
	c, err := Load(os.DirFS("testdata/win"))
	require.NoError(t, err)
	regs := c["x"].Registry
	require.Len(t, regs, 3)
	assert.Equal(t, RegRestore{Mode: "delete"}, regs[0].Restore)
	assert.Equal(t, RegRestore{Mode: "value", Value: 1}, regs[1].Restore)
	assert.Equal(t, "keep", regs[2].Restore.Mode)
	assert.True(t, c["x"].Windows())
}

func TestValidateRestoreRequired(t *testing.T) {
	v := dw(`HKLM\X`, "A", 1)
	v.Restore = RegRestore{}
	errs := Validate(winRecipe("vm", "", v), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], `HKLM\X\A`)
	assert.ErrorContains(t, errs[0], "restore")
}

func TestValidateRestoreValueNeedsValue(t *testing.T) {
	v := dw(`HKLM\X`, "A", 1)
	v.Restore = RegRestore{Mode: "value"}
	errs := Validate(winRecipe("vm", "", v), winDefs())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "restore")
	v.Restore.Value = 0
	assert.Empty(t, Validate(winRecipe("vm", "", v), winDefs()))
}

func TestValidateLinuxRecipeRejectsPs1(t *testing.T) {
	c := Catalog{"x": {Name: "x", Venue: "vm", HasPs1: true}}
	errs := Validate(c, platform.Defs{})
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "recipe.ps1")
	assert.ErrorContains(t, errs[0], "windows")
}
