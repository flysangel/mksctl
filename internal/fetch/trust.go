package fetch

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TrustEntry 是 trust/hashes.yaml 裡自行記錄的一筆雜湊。
type TrustEntry struct {
	URL    string `yaml:"url"`
	Size   int64  `yaml:"size"`
	SHA256 string `yaml:"sha256"`
}

// TrustDB 的鍵是給人看的名稱，查詢一律用 URL，鍵怎麼取不影響比對。
type TrustDB map[string]TrustEntry

// LoadTrust 解析 trust/hashes.yaml。
func LoadTrust(data []byte) (TrustDB, error) {
	var db TrustDB
	if err := yaml.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("fetch: 解析 trust：%w", err)
	}
	for k, e := range db {
		if e.URL == "" || len(e.SHA256) != 64 {
			return nil, fmt.Errorf("fetch: trust 項目 %q 缺 url 或 sha256 長度不對", k)
		}
	}
	return db, nil
}

// lookup 依下載網址找項目。
func (db TrustDB) lookup(url string) (TrustEntry, bool) {
	for _, e := range db {
		if e.URL == url {
			e.SHA256 = strings.ToLower(e.SHA256)
			return e, true
		}
	}
	return TrustEntry{}, false
}
