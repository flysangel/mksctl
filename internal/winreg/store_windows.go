//go:build windows

package winreg

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// systemStore 直接讀寫 Windows 登錄區（原生位元數的檢視，不走 WOW6432Node 重導向）。
type systemStore struct{}

// NewSystemStore 回傳操作真實登錄區的 Store。
func NewSystemStore() Store { return systemStore{} }

// Default 在 Windows 回傳真實登錄區。
func Default() Store { return NewSystemStore() }

func open(key string, access uint32) (registry.Key, error) {
	root, path, err := splitKey(key)
	if err != nil {
		return 0, err
	}
	var r registry.Key
	switch root {
	case "HKLM":
		r = registry.LOCAL_MACHINE
	case "HKCU":
		r = registry.CURRENT_USER
	case "HKCR":
		r = registry.CLASSES_ROOT
	}
	if path == "" {
		return r, nil
	}
	return registry.OpenKey(r, path, access)
}

func (systemStore) KeyExists(key string) (bool, error) {
	k, err := open(key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	k.Close()
	return true, nil
}

func (systemStore) Get(key, name string) (string, any, bool, error) {
	k, err := open(key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	defer k.Close()
	_, vt, err := k.GetValue(name, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	switch vt {
	case registry.SZ, registry.EXPAND_SZ:
		s, _, err := k.GetStringValue(name)
		typ := "string"
		if vt == registry.EXPAND_SZ {
			typ = "expand_string"
		}
		return typ, s, err == nil, err
	case registry.MULTI_SZ:
		s, _, err := k.GetStringsValue(name)
		return "multi_string", s, err == nil, err
	case registry.DWORD:
		n, _, err := k.GetIntegerValue(name)
		return "dword", uint32(n), err == nil, err
	case registry.QWORD:
		n, _, err := k.GetIntegerValue(name)
		return "qword", n, err == nil, err
	}
	return "", nil, false, fmt.Errorf("不支援的登錄值型別 %d", vt)
}

func (systemStore) Set(key, name, typ string, val any) error {
	root, path, err := splitKey(key)
	if err != nil {
		return err
	}
	var r registry.Key
	switch root {
	case "HKLM":
		r = registry.LOCAL_MACHINE
	case "HKCU":
		r = registry.CURRENT_USER
	case "HKCR":
		r = registry.CLASSES_ROOT
	}
	k, _, err := registry.CreateKey(r, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch typ {
	case "string":
		return k.SetStringValue(name, val.(string))
	case "expand_string":
		return k.SetExpandStringValue(name, val.(string))
	case "multi_string":
		return k.SetStringsValue(name, val.([]string))
	case "dword":
		return k.SetDWordValue(name, val.(uint32))
	case "qword":
		return k.SetQWordValue(name, val.(uint64))
	}
	return fmt.Errorf("未知的登錄值型別 %q", typ)
}

func (systemStore) Delete(key, name string) error {
	k, err := open(key, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
