// Package pkgindex 更新並查詢系統套件庫索引（apt 或 apk）。
package pkgindex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/flysangel/mksctl/internal/platform"
)

// Index 是套件庫索引。
type Index interface {
	Refresh(ctx context.Context) error
	Has(ctx context.Context, pkg string) (bool, error)
}

// Runner 執行外部指令。err 只代表「指令起不來」，非零結束碼走 exitCode。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (exitCode int, out []byte, err error)
}

// ExecRunner 是用 os/exec 的真實實作。
// Env 非空時附加在目前環境之後（apt 用它帶 DEBIAN_FRONTEND=noninteractive）。
type ExecRunner struct {
	Env []string
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (int, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), buf.Bytes(), nil
	}
	if err != nil {
		return -1, nil, err
	}
	return 0, buf.Bytes(), nil
}

// For 依層名選實作：有 apt 層用 apt，有 apk 層用 apk，其餘回傳所有方法都報錯的 Index。
func For(p platform.Platform) Index {
	for _, l := range p.Layers {
		switch l {
		case "apt":
			return &aptIndex{r: ExecRunner{Env: []string{"DEBIAN_FRONTEND=noninteractive"}}}
		case "apk":
			return &apkIndex{r: ExecRunner{}}
		}
	}
	return unsupported{}
}

type unsupported struct{}

var errUnsupported = errors.New("pkgindex: 這個平台沒有 apt 或 apk 層，無法查套件庫")

func (unsupported) Refresh(context.Context) error             { return errUnsupported }
func (unsupported) Has(context.Context, string) (bool, error) { return false, errUnsupported }

// tail 只留輸出尾端，錯誤訊息才不會被整份 apt 輸出灌爆。
func tail(b []byte) string {
	const max = 2000
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	return s
}

type aptIndex struct{ r Runner }

func (a *aptIndex) Refresh(ctx context.Context) error {
	// DPkg::Lock::Timeout 讓開機初期 unattended-upgrades 占鎖時等待而非立刻失敗。
	code, out, err := a.r.Run(ctx, "apt-get", "update", "-o", "DPkg::Lock::Timeout=300")
	if err != nil {
		return fmt.Errorf("pkgindex: 執行 apt-get update 失敗：%w", err)
	}
	if code != 0 {
		return fmt.Errorf("pkgindex: apt-get update 結束碼 %d：%s", code, tail(out))
	}
	return nil
}

func (a *aptIndex) Has(ctx context.Context, pkg string) (bool, error) {
	code, out, err := a.r.Run(ctx, "apt-cache", "show", pkg)
	if err != nil {
		return false, fmt.Errorf("pkgindex: 執行 apt-cache 失敗：%w", err)
	}
	switch code {
	case 0:
		return true, nil
	case 100: // apt 對「找不到套件」回 100
		return false, nil
	}
	return false, fmt.Errorf("pkgindex: apt-cache show %s 結束碼 %d：%s", pkg, code, tail(out))
}

type apkIndex struct{ r Runner }

func (a *apkIndex) Refresh(ctx context.Context) error {
	code, out, err := a.r.Run(ctx, "apk", "update")
	if err != nil {
		return fmt.Errorf("pkgindex: 執行 apk update 失敗：%w", err)
	}
	if code != 0 {
		return fmt.Errorf("pkgindex: apk update 結束碼 %d：%s", code, tail(out))
	}
	return nil
}

func (a *apkIndex) Has(ctx context.Context, pkg string) (bool, error) {
	code, out, err := a.r.Run(ctx, "apk", "search", "-x", pkg)
	if err != nil {
		return false, fmt.Errorf("pkgindex: 執行 apk search 失敗：%w", err)
	}
	if code != 0 {
		return false, fmt.Errorf("pkgindex: apk search %s 結束碼 %d：%s", pkg, code, tail(out))
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			return true, nil
		}
	}
	return false, nil
}
