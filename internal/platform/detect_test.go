package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetect(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	must(os.WriteFile(filepath.Join(root, "etc/os-release"),
		[]byte("ID=ubuntu\nVERSION_ID=\"26.04\"\nVERSION_CODENAME=resolute\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755))

	p, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Parse([]byte("ID=ubuntu\nVERSION_ID=\"26.04\"\nVERSION_CODENAME=resolute\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Layers, want.Layers) {
		t.Errorf("Layers = %v，要 %v", p.Layers, want.Layers)
	}
	if !p.InitRunning {
		t.Error("InitRunning 應為 true")
	}
}

func TestDetectNoOSRelease(t *testing.T) {
	if _, err := Detect(t.TempDir()); err == nil {
		t.Fatal("沒有 os-release 應回錯誤")
	}
}
