//go:build windows

package runner

import "os/exec"

// configureProc 在 Windows 上不做事：Windows 平台尚未支援，這裡只求能編譯。
func configureProc(cmd *exec.Cmd) {}

// killProc 在 Windows 上只殺主行程，不處理子行程。
func killProc(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
