//go:build windows

package runner

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// newKillOnCloseJob 建一個 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 的 Job Object。
// 最後一個 handle 關閉時，Windows 會殺掉 job 內所有行程，含子孫。
func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("建立 Job Object 失敗：%w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("設定 Job Object 失敗：%w", err)
	}
	return job, nil
}

// assignPID 把已啟動的行程加入 job。
func assignPID(job windows.Handle, pid int) error {
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE
	h, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("開啟行程失敗：%w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		return fmt.Errorf("加入 Job Object 失敗：%w", err)
	}
	return nil
}
