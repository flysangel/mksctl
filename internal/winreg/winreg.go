// Package winreg 套用、回讀與還原宣告式的 Windows 登錄值。
// 還原的對稱性由引擎保證：每個值都宣告 restore，Restore 照宣告做。
package winreg

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Value 是一筆宣告式登錄值。
type Value struct {
	Key             string      `yaml:"key"`
	Name            string      `yaml:"name"`
	Type            string      `yaml:"type"`
	Value           any         `yaml:"value"`
	Ensure          string      `yaml:"ensure"` // 空字串或 present 是寫入；absent 是確保不存在
	Restore         RestoreSpec `yaml:"restore"`
	Reason          string      `yaml:"reason"`
	OnlyIfKeyExists bool        `yaml:"only_if_key_exists"`
}

// RestoreSpec 是還原方式。Mode 為 delete、value 或 keep；value 時 Value 是要寫回的值。
type RestoreSpec struct {
	Mode  string
	Value any
}

// UnmarshalYAML 接受純量 delete／keep，或 {value: <v>}。
func (r *RestoreSpec) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "delete" || n.Value == "keep" {
			*r = RestoreSpec{Mode: n.Value}
			return nil
		}
	case yaml.MappingNode:
		if len(n.Content) == 2 && n.Content[0].Value == "value" {
			var v any
			if err := n.Content[1].Decode(&v); err != nil {
				return err
			}
			*r = RestoreSpec{Mode: "value", Value: v}
			return nil
		}
	}
	return fmt.Errorf("第 %d 行：restore 只接受 delete、keep 或 {value: <值>}", n.Line)
}

// Result 是單筆值的處理結果。Skipped 表示 only_if_key_exists 且鍵不存在，沒有動作。
type Result struct {
	Key, Name string
	Skipped   bool
}

func (v Value) id() string { return v.Key + `\` + v.Name }

// skip 判斷 only_if_key_exists 的略過條件。
func skip(s Store, v Value) (bool, error) {
	if !v.OnlyIfKeyExists {
		return false, nil
	}
	ok, err := s.KeyExists(v.Key)
	return !ok, err
}

// Apply 依序寫入（或刪除 ensure: absent 的）每個值。遇到第一個錯誤就停。
func Apply(s Store, vals []Value) ([]Result, error) {
	var out []Result
	for _, v := range vals {
		sk, err := skip(s, v)
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
		out = append(out, Result{v.Key, v.Name, sk})
		if sk {
			continue
		}
		if v.Ensure == "absent" {
			err = s.Delete(v.Key, v.Name)
		} else {
			var nv any
			if nv, err = normalize(v.Type, v.Value); err == nil {
				err = s.Set(v.Key, v.Name, v.Type, nv)
			}
		}
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
	}
	return out, nil
}

// Verify 回讀每個值，型別或值不符就回錯誤；ensure: absent 的值存在也是錯誤。
func Verify(s Store, vals []Value) ([]Result, error) {
	var out []Result
	for _, v := range vals {
		sk, err := skip(s, v)
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
		out = append(out, Result{v.Key, v.Name, sk})
		if sk {
			continue
		}
		typ, got, ok, err := s.Get(v.Key, v.Name)
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
		if v.Ensure == "absent" {
			if ok {
				return out, fmt.Errorf("%s 應不存在，實際為 %s", v.id(), show(got))
			}
			continue
		}
		want, err := normalize(v.Type, v.Value)
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
		if !ok {
			return out, fmt.Errorf("%s 應為 %s，實際不存在", v.id(), show(want))
		}
		g, gerr := normalize(typ, got)
		if typ != v.Type || gerr != nil || !reflect.DeepEqual(g, want) {
			if typ != v.Type {
				return out, fmt.Errorf("%s 應為 %s %s，實際為 %s %s", v.id(), v.Type, show(want), typ, show(got))
			}
			return out, fmt.Errorf("%s 應為 %s，實際為 %s", v.id(), show(want), show(got))
		}
	}
	return out, nil
}

// Restore 照每個值的 restore 宣告還原。delete 刪值（不存在不算錯）、value 寫回、keep 不動。
// ensure: absent 的值不還原。
func Restore(s Store, vals []Value) ([]Result, error) {
	var out []Result
	for _, v := range vals {
		sk, err := skip(s, v)
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
		out = append(out, Result{v.Key, v.Name, sk})
		if sk || v.Ensure == "absent" {
			continue
		}
		switch v.Restore.Mode {
		case "delete":
			err = s.Delete(v.Key, v.Name)
		case "value":
			var nv any
			if nv, err = normalize(v.Type, v.Restore.Value); err == nil {
				err = s.Set(v.Key, v.Name, v.Type, nv)
			}
		case "keep":
		default:
			err = fmt.Errorf("未知的 restore 模式 %q", v.Restore.Mode)
		}
		if err != nil {
			return out, fmt.Errorf("%s：%w", v.id(), err)
		}
	}
	return out, nil
}

// VerifyRestored 確認還原後的狀態：delete 的值不存在，value 的值等於寫回值，keep 不檢查。
func VerifyRestored(s Store, vals []Value) error {
	for _, v := range vals {
		sk, err := skip(s, v)
		if err != nil {
			return fmt.Errorf("%s：%w", v.id(), err)
		}
		if sk || v.Ensure == "absent" {
			continue
		}
		typ, got, ok, err := s.Get(v.Key, v.Name)
		if err != nil {
			return fmt.Errorf("%s：%w", v.id(), err)
		}
		switch v.Restore.Mode {
		case "delete":
			if ok {
				return fmt.Errorf("%s 還原後應不存在，實際為 %s", v.id(), show(got))
			}
		case "value":
			want, err := normalize(v.Type, v.Restore.Value)
			if err != nil {
				return fmt.Errorf("%s：%w", v.id(), err)
			}
			if !ok {
				return fmt.Errorf("%s 還原後應為 %s，實際不存在", v.id(), show(want))
			}
			g, gerr := normalize(typ, got)
			if typ != v.Type || gerr != nil || !reflect.DeepEqual(g, want) {
				return fmt.Errorf("%s 還原後應為 %s，實際為 %s", v.id(), show(want), show(got))
			}
		}
	}
	return nil
}

func show(v any) string {
	if s, ok := v.([]string); ok {
		return "[" + strings.Join(s, ", ") + "]"
	}
	return fmt.Sprintf("%v", v)
}

// normalize 把 YAML 載入的值轉成登錄區實際儲存的型別。
func normalize(typ string, v any) (any, error) {
	switch typ {
	case "string", "expand_string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s 需要字串，實際為 %T", typ, v)
		}
		return s, nil
	case "dword", "qword":
		n, err := toUint(v)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", typ, err)
		}
		if typ == "dword" {
			if n > math.MaxUint32 {
				return nil, fmt.Errorf("dword 值 %d 超出範圍", n)
			}
			return uint32(n), nil
		}
		return n, nil
	case "multi_string":
		switch x := v.(type) {
		case []string:
			return append([]string(nil), x...), nil
		case []any:
			out := make([]string, 0, len(x))
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("multi_string 的元素需要字串，實際為 %T", e)
				}
				out = append(out, s)
			}
			return out, nil
		}
		return nil, fmt.Errorf("multi_string 需要字串陣列，實際為 %T", v)
	}
	return nil, fmt.Errorf("未知的登錄值型別 %q", typ)
}

func toUint(v any) (uint64, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.Int() < 0 {
			return 0, fmt.Errorf("不接受負值 %d", rv.Int())
		}
		return uint64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), nil
	}
	return 0, fmt.Errorf("需要整數，實際為 %T", v)
}
