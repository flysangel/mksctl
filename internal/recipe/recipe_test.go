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
