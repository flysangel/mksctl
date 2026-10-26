package platform

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDefs(t *testing.T) Defs {
	t.Helper()
	d, err := LoadDefs(fstest.MapFS{
		"platforms/windows.yaml":        {Data: []byte("provides: [windows]\n")},
		"platforms/windows-client.yaml": {Data: []byte("provides: []\n")},
		"platforms/windows-11.yaml":     {Data: []byte("provides: []\n")},
	})
	require.NoError(t, err)
	return d
}

func TestWindowsPlatformTable(t *testing.T) {
	assert.Equal(t, []string{"windows-server-2025", "windows-server", "windows"}, WindowsPlatform("Server", "26100").Layers)
	assert.Equal(t, []string{"windows-11", "windows-client", "windows"}, WindowsPlatform("Client", "26100").Layers)
	assert.Equal(t, []string{"windows-11", "windows-client", "windows"}, WindowsPlatform("Client", "26200").Layers)
	assert.Equal(t, []string{"windows-10", "windows-client", "windows"}, WindowsPlatform("Client", "19045").Layers)
	assert.Equal(t, []string{"windows-server-2025", "windows-server", "windows"}, WindowsPlatform("Server Core", "26100").Layers)
}

func TestWindowsPlatformUnknownBuild(t *testing.T) {
	p := testDefs(t).Apply(WindowsPlatform("Client", "27000"))
	assert.False(t, p.HasExactLayer)
	assert.Equal(t, []string{"windows-client-27000", "windows-client", "windows"}, p.Layers)
	assert.True(t, p.Provides["windows"])

	s := WindowsPlatform("Server", "27000")
	assert.Equal(t, []string{"windows-server-27000", "windows-server", "windows"}, s.Layers)
}

func TestWindowsPlatformUnknownInstallationType(t *testing.T) {
	p := WindowsPlatform("Nano", "26100")
	assert.Equal(t, "windows-unknown-26100", p.Layers[0])
	assert.Equal(t, "windows", p.Layers[len(p.Layers)-1])
}

func TestWindowsPlatformRuntime(t *testing.T) {
	p := WindowsPlatform("Client", "26100")
	assert.Equal(t, "windows", p.ID)
	assert.Equal(t, "26100", p.Version)
	assert.True(t, p.InitRunning)
	assert.False(t, p.InContainer)

	p = testDefs(t).Apply(p)
	assert.True(t, p.HasExactLayer)
	assert.True(t, p.Provides["init"])
	assert.True(t, p.Provides["kernel"])
}

func TestWindowsLayerFor(t *testing.T) {
	typ, build, ok := WindowsLayerFor("windows-11")
	assert.True(t, ok)
	assert.Equal(t, "Client", typ)
	assert.Equal(t, "26100", build)

	typ, build, ok = WindowsLayerFor("windows-server-2025")
	assert.True(t, ok)
	assert.Equal(t, "Server", typ)
	assert.Equal(t, "26100", build)

	_, _, ok = WindowsLayerFor("windows-95")
	assert.False(t, ok)
}

func TestWindowsPlatformUnsupportedBuildsHaveNoExactLayer(t *testing.T) {
	defs := testDefs(t)
	for _, c := range []struct{ typ, build, layer string }{
		{"Client", "22631", "windows-client-22631"},
		{"Client", "19044", "windows-client-19044"},
		{"Server", "20348", "windows-server-20348"},
	} {
		p := defs.Apply(WindowsPlatform(c.typ, c.build))
		assert.Equal(t, c.layer, p.Layers[0])
		assert.False(t, p.HasExactLayer, c.layer)
	}
}
