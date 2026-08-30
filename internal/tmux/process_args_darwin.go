//go:build darwin

package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ctlKern         = 1
	kernProcArgs2   = 49
	procPIDInfo     = 2
	procPIDTBSDInfo = 3
)

type procBSDInfo struct {
	Flags, Status, XStatus, PID, PPID, UID, GID, RUID, RGID, SVUID, SVGID, Reserved uint32
	Comm                                                                            [16]byte
	Name                                                                            [32]byte
	NFiles, PGID, PJobC, TDev, TPGID                                                uint32
	Nice                                                                            int32
	StartSeconds, StartMicroseconds                                                 uint64
}

var (
	inspectProcBSDInfo = readProcBSDInfo
	sysctlKinfoProc    = unix.SysctlKinfoProc
)

// ProcessArgs returns the kernel's exact argv vector for pid.
func ProcessArgs(pid int) ([]string, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("process PID is unavailable")
	}
	mib := [...]int32{ctlKern, kernProcArgs2, int32(pid)}
	var size uintptr
	if _, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), 0, uintptr(unsafe.Pointer(&size)), 0, 0); errno != 0 {
		return nil, fmt.Errorf("inspect process %d argv size: %w", pid, errno)
	}
	if size < unsafe.Sizeof(int32(0)) {
		return nil, fmt.Errorf("process %d returned incomplete argv", pid)
	}
	data := make([]byte, size)
	if _, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, 0); errno != 0 {
		return nil, fmt.Errorf("inspect process %d argv: %w", pid, errno)
	}
	data = data[:size]
	argc := int(*(*int32)(unsafe.Pointer(&data[0])))
	if argc <= 0 {
		return nil, fmt.Errorf("process %d returned invalid argc %d", pid, argc)
	}
	data = data[unsafe.Sizeof(int32(0)):]
	endExecutable := bytes.IndexByte(data, 0)
	if endExecutable < 0 {
		return nil, fmt.Errorf("process %d returned malformed executable path", pid)
	}
	data = data[endExecutable+1:]
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}
	args := make([]string, 0, argc)
	for len(args) < argc {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, fmt.Errorf("process %d returned incomplete argv", pid)
		}
		args = append(args, string(data[:end]))
		data = data[end+1:]
	}
	return args, nil
}

// ProcessIdentity returns Darwin's native per-incarnation process start time.
func ProcessIdentity(pid int) (string, error) {
	link, err := InspectProcessLink(pid)
	if err != nil {
		return "", err
	}
	return link.Identity, nil
}

// InspectProcessLink returns parent and per-incarnation identity from one
// native proc_bsdinfo snapshot.
func InspectProcessLink(pid int) (ProcessMetadata, error) {
	if pid <= 0 {
		return ProcessMetadata{}, fmt.Errorf("process PID is unavailable")
	}
	info, err := inspectProcBSDInfo(pid)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			return inspectProcessLinkSysctl(pid)
		}
		return ProcessMetadata{}, err
	}
	if info.StartSeconds == 0 || info.StartMicroseconds >= 1_000_000 {
		return ProcessMetadata{}, fmt.Errorf("process %d returned incomplete ancestry identity", pid)
	}
	return ProcessMetadata{PID: pid, ParentPID: int(info.PPID), Identity: fmt.Sprintf("%d.%06d", info.StartSeconds, info.StartMicroseconds)}, nil
}

func inspectProcessLinkSysctl(pid int) (ProcessMetadata, error) {
	info, err := sysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ProcessMetadata{}, fmt.Errorf("inspect process %d fallback metadata: %w", pid, err)
	}
	if int(info.Proc.P_pid) != pid || info.Eproc.Ppid < 0 || info.Proc.P_starttime.Sec <= 0 || info.Proc.P_starttime.Usec < 0 || info.Proc.P_starttime.Usec >= 1_000_000 {
		return ProcessMetadata{}, fmt.Errorf("process %d returned incomplete fallback ancestry identity", pid)
	}
	return ProcessMetadata{
		PID:       pid,
		ParentPID: int(info.Eproc.Ppid),
		Identity:  fmt.Sprintf("%d.%06d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec),
	}, nil
}

// ProcessName returns Darwin's native comm value without normalizing whitespace.
func ProcessName(pid int) (string, error) {
	info, err := readProcBSDInfo(pid)
	if err != nil {
		return "", err
	}
	end := bytes.IndexByte(info.Comm[:], 0)
	if end < 0 {
		end = len(info.Comm)
	}
	if end == 0 {
		return "", fmt.Errorf("process %d returned empty name", pid)
	}
	return string(info.Comm[:end]), nil
}

func readProcBSDInfo(pid int) (procBSDInfo, error) {
	if pid <= 0 {
		return procBSDInfo{}, fmt.Errorf("process PID is unavailable")
	}
	var info procBSDInfo
	size := unsafe.Sizeof(info)
	written, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, procPIDInfo, uintptr(pid), procPIDTBSDInfo, 0, uintptr(unsafe.Pointer(&info)), size)
	if errno != 0 {
		return procBSDInfo{}, fmt.Errorf("inspect process %d metadata: %w", pid, errno)
	}
	if written != size || info.PID != uint32(pid) {
		return procBSDInfo{}, fmt.Errorf("process %d returned incomplete metadata", pid)
	}
	return info, nil
}
