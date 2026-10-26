//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// configureProc 讓子行程自成一組行程組。
// 配方會產生子行程（例如 sleep）；只殺 bash 的話，子行程握著輸出管線，
// Wait 會卡到它自己結束。所以自成一組，取消時整組一起殺（見 killProc）。
func configureProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProc 殺掉整個行程組。
func killProc(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
