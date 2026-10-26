// Package pkgmap 把配方的邏輯套件名稱對照成各平台的實際套件名稱。
package pkgmap

import (
	"errors"

	"gopkg.in/yaml.v3"
)

// ErrUnavailable 表示該平台明確沒有這個套件（YAML 值為 null）。
var ErrUnavailable = errors.New("此平台沒有這個套件")

// Map 的鍵是邏輯名稱，內層鍵是平台層名。
// 值用 *[]string：nil 指標代表 YAML 的 null（明確不提供），
// 鍵不存在則代表沒有意見、換下一層找。兩者不能混為一談。
type Map map[string]map[string]*[]string

// Load 解析套件對照表。
func Load(data []byte) (Map, error) {
	var m Map
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Lookup 取 layers 中第一個有定義的層。
// 邏輯名稱不在表裡、或沒有任何層命中時，原樣當成套件名稱：
// 多數套件在各平台同名，不必逐一登記。
func (m Map) Lookup(logical string, layers []string) ([]string, error) {
	byLayer, ok := m[logical]
	if !ok {
		return []string{logical}, nil
	}
	for _, l := range layers {
		v, ok := byLayer[l]
		if !ok {
			continue
		}
		if v == nil {
			return nil, ErrUnavailable
		}
		return *v, nil
	}
	return []string{logical}, nil
}
