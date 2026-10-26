package fetch

import (
	"fmt"
	"os"
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

// MergeInto 把 add 併進 path 的 trust 檔：同鍵取代，其餘保留。yaml.v3 輸出時鍵按字母排序，所以順序固定；
// 檔頭連續的註解與空行原樣保留。寫入先寫同目錄暫存檔再 rename，中途失敗不會留下半個檔。
func MergeInto(path string, add TrustDB) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("讀 %s（要在 repo 根執行）：%w", path, err)
	}
	db, err := LoadTrust(old)
	if err != nil {
		return err
	}
	if db == nil {
		db = TrustDB{}
	}
	for k, v := range add {
		db[k] = v
	}
	var header strings.Builder
	for _, line := range strings.SplitAfter(string(old), "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		header.WriteString(line)
	}
	body, err := yaml.Marshal(db)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header.String()), body...), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
