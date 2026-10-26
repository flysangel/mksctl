//go:build windows

package runner

import (
	"os/exec"
	"sync"

	"golang.org/x/sys/windows"
)

// jobs 記下每個 cmd 對應的 Job Object handle，取消時靠它整棵殺掉。
var jobs sync.Map // *exec.Cmd -> windows.Handle

// configureProc 在 Windows 上不用在啟動前設旗標：子行程必須先存在才能加入 job，
// 加入的動作在 afterStart。
func configureProc(cmd *exec.Cmd) {}

// afterStart 在 cmd.Start 之後呼叫：把子行程加入 KILL_ON_JOB_CLOSE 的 Job Object。
// 加入失敗就殺掉子行程並回錯誤，不讓配方在沒有行程樹保護的情況下跑。
func afterStart(cmd *exec.Cmd) error {
	job, err := newKillOnCloseJob()
	if err != nil {
		cmd.Process.Kill()
		return err
	}
	if err := assignPID(job, cmd.Process.Pid); err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return err
	}
	jobs.Store(cmd, job)
	return nil
}

// afterWait 在 cmd.Wait 之後呼叫：關閉 job handle。此時 job 內若還有殘留行程，一併被殺。
func afterWait(cmd *exec.Cmd) {
	if h, ok := jobs.LoadAndDelete(cmd); ok {
		windows.CloseHandle(h.(windows.Handle))
	}
}

// killProc 關閉 job handle，Windows 殺掉 job 內整棵行程樹。
func killProc(cmd *exec.Cmd) error {
	if h, ok := jobs.LoadAndDelete(cmd); ok {
		return windows.CloseHandle(h.(windows.Handle))
	}
	return cmd.Process.Kill()
}
