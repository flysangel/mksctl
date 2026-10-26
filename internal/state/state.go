// Package state 記錄各配方裝了哪個版本，供重跑時判斷要不要略過。
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/flysangel/mksctl/internal/recipe"
)

// Entry 是單一配方的安裝紀錄。
type Entry struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
}

// File 是整份狀態檔。
type File struct {
	Recipes map[string]Entry `json:"recipes"`
}

// Action 是 Decide 的結論。
type Action int

const (
	// Run 表示要執行配方。
	Run Action = iota
	// Skip 表示已是最新，略過。
	Skip
)

// Decide 只在配方有版本、且紀錄的版本等於 latest 時略過。
// 無版本的配方沒有「最新」可比，一律重跑，讓配方自己保持冪等。
func Decide(f File, r recipe.Recipe, latest string) Action {
	if !r.Versioned {
		return Run
	}
	if e, ok := f.Recipes[r.Name]; ok && e.Version == latest {
		return Skip
	}
	return Run
}

// Load 讀狀態檔。檔不存在回空的 File；內容壞掉回錯誤，
// 不默默重置，否則會把已裝的紀錄清掉、造成重複安裝。
func Load(path string) (File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{Recipes: map[string]Entry{}}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("狀態檔 %s 內容壞掉：%w", path, err)
	}
	if f.Recipes == nil {
		f.Recipes = map[string]Entry{}
	}
	return f, nil
}

// Save 先寫同目錄的暫存檔再 rename，中途斷電也不會留下半份狀態檔。
func (f File) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // rename 成功後已不存在，失敗時負責清掉。
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp 預設 0600，狀態檔要 0644。
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
