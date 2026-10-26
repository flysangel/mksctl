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
	// Data 是配方自己的資料（例如 windowsuser 建了哪些帳號），引擎原樣保存、不解讀。
	// 配方經 MKSCTL_STATE_IN 讀、經 MKSCTL_STATE_OUT 寫，見 install 套件。
	Data json.RawMessage `json:"data,omitempty"`
}

// File 是整份狀態檔。
type File struct {
	Recipes map[string]Entry `json:"recipes"`
}

// Delete 移除一個配方的紀錄。沒有紀錄不算錯：restore 沒裝過的配方也要能成功。
func (f File) Delete(name string) { delete(f.Recipes, name) }

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
	// 統一存 UTC，不同時區的機器寫出的檔才一致。
	rs := make(map[string]Entry, len(f.Recipes))
	for k, e := range f.Recipes {
		e.InstalledAt = e.InstalledAt.UTC()
		rs[k] = e
	}
	b, err := json.MarshalIndent(File{Recipes: rs}, "", "  ")
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
	return renameRetry(tmp.Name(), path, os.Rename, renameRetryable, renameAttempts, renameInterval)
}

// rename 重試的次數與間隔。Windows 上防毒或索引服務短暫開著目標檔時，
// rename 會回存取被拒或共用違規，等一下通常就好。
const (
	renameAttempts = 10
	renameInterval = 100 * time.Millisecond
)

// renameRetry 呼叫 rename，遇到 retryable 判定為暫時性的錯誤時重試，最多 attempts 次。
// 判定函式依平台不同（見 save_windows.go），重試邏輯放這裡，Linux 上也測得到。
func renameRetry(from, to string, rename func(string, string) error, retryable func(error) bool,
	attempts int, interval time.Duration,
) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = rename(from, to); err == nil || !retryable(err) {
			return err
		}
		if i < attempts-1 {
			time.Sleep(interval)
		}
	}
	return err
}
