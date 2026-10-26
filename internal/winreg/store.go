package winreg

import (
	"fmt"
	"strings"
	"sync"
)

// Store 是登錄區的最小介面。key 用 `HKLM\…` 形式，接受 HKLM、HKCU、HKCR 三個根。
// typ 用 string、expand_string、multi_string、dword、qword。
type Store interface {
	KeyExists(key string) (bool, error)
	Get(key, name string) (typ string, val any, ok bool, err error)
	Set(key, name, typ string, val any) error
	// Delete 刪值；值或鍵不存在不算錯。
	Delete(key, name string) error
}

// splitKey 拆出大寫的根與其餘路徑，根不在白名單就回錯誤。
func splitKey(key string) (root, path string, err error) {
	root, path, _ = strings.Cut(key, `\`)
	root = strings.ToUpper(root)
	switch root {
	case "HKLM", "HKCU", "HKCR":
		return root, path, nil
	}
	return "", "", fmt.Errorf("不支援的登錄根 %q（只接受 HKLM、HKCU、HKCR）", key)
}

// MemStore 是記憶體內的 Store，鍵名大小寫不分，與真實登錄區一致。
type MemStore struct {
	mu   sync.Mutex
	keys map[string]map[string]memVal // 正規化鍵 -> 小寫值名 -> 值
}

type memVal struct {
	typ string
	val any
}

func NewMemStore() *MemStore {
	return &MemStore{keys: map[string]map[string]memVal{}}
}

func memKey(key string) (string, error) {
	root, path, err := splitKey(key)
	if err != nil {
		return "", err
	}
	return strings.ToLower(root + `\` + strings.Trim(path, `\`)), nil
}

func (m *MemStore) KeyExists(key string) (bool, error) {
	mk, err := memKey(key)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.keys[mk]
	return ok, nil
}

func (m *MemStore) Get(key, name string) (string, any, bool, error) {
	mk, err := memKey(key)
	if err != nil {
		return "", nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.keys[mk][strings.ToLower(name)]
	return v.typ, v.val, ok, nil
}

func (m *MemStore) Set(key, name, typ string, val any) error {
	mk, err := memKey(key)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys[mk] == nil {
		m.keys[mk] = map[string]memVal{}
	}
	m.keys[mk][strings.ToLower(name)] = memVal{typ, val}
	return nil
}

func (m *MemStore) Delete(key, name string) error {
	mk, err := memKey(key)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys[mk], strings.ToLower(name))
	return nil
}
