//go:build darwin

package tmux

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInspectProcessLinkFallsBackToSysctlOnPermissionDenied(t *testing.T) {
	oldInspect, oldSysctl := inspectProcBSDInfo, sysctlKinfoProc
	permissionDenied := false
	inspectProcBSDInfo = func(int) (procBSDInfo, error) {
		if permissionDenied {
			return procBSDInfo{}, fmt.Errorf("inspect metadata: %w", syscall.EPERM)
		}
		return procBSDInfo{PID: 3794, PPID: 565, StartSeconds: 123, StartMicroseconds: 456}, nil
	}
	sysctlKinfoProc = func(name string, args ...int) (*unix.KinfoProc, error) {
		if name != "kern.proc.pid" || len(args) != 1 || args[0] != 3794 {
			t.Fatalf("sysctl lookup = %q %v", name, args)
		}
		return &unix.KinfoProc{
			Proc: unix.ExternProc{
				P_pid:       3794,
				P_starttime: unix.Timeval{Sec: 123, Usec: 456},
			},
			Eproc: unix.Eproc{Ppid: 565},
		}, nil
	}
	t.Cleanup(func() {
		inspectProcBSDInfo, sysctlKinfoProc = oldInspect, oldSysctl
	})

	primary, err := InspectProcessLink(3794)
	if err != nil {
		t.Fatal(err)
	}
	permissionDenied = true
	fallback, err := InspectProcessLink(3794)
	if err != nil {
		t.Fatal(err)
	}
	if fallback != primary || fallback.PID != 3794 || fallback.ParentPID != 565 || fallback.Identity != "123.000456" {
		t.Fatalf("primary metadata = %+v, fallback metadata = %+v", primary, fallback)
	}
}

func TestInspectProcessLinkRejectsMalformedStartTime(t *testing.T) {
	oldInspect := inspectProcBSDInfo
	inspectProcBSDInfo = func(int) (procBSDInfo, error) {
		return procBSDInfo{PID: 3794, PPID: 565, StartSeconds: 123, StartMicroseconds: 1_000_000}, nil
	}
	t.Cleanup(func() { inspectProcBSDInfo = oldInspect })

	if _, err := InspectProcessLink(3794); err == nil {
		t.Fatal("malformed primary start time was accepted")
	}
}

func TestInspectProcessLinkRejectsMalformedFallbackMetadata(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*unix.KinfoProc)
	}{
		{name: "wrong PID", edit: func(info *unix.KinfoProc) { info.Proc.P_pid++ }},
		{name: "negative parent PID", edit: func(info *unix.KinfoProc) { info.Eproc.Ppid = -1 }},
		{name: "zero start seconds", edit: func(info *unix.KinfoProc) { info.Proc.P_starttime.Sec = 0 }},
		{name: "negative start microseconds", edit: func(info *unix.KinfoProc) { info.Proc.P_starttime.Usec = -1 }},
		{name: "overflowing start microseconds", edit: func(info *unix.KinfoProc) { info.Proc.P_starttime.Usec = 1_000_000 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldInspect, oldSysctl := inspectProcBSDInfo, sysctlKinfoProc
			inspectProcBSDInfo = func(int) (procBSDInfo, error) {
				return procBSDInfo{}, syscall.EPERM
			}
			sysctlKinfoProc = func(string, ...int) (*unix.KinfoProc, error) {
				info := &unix.KinfoProc{
					Proc:  unix.ExternProc{P_pid: 3794, P_starttime: unix.Timeval{Sec: 123, Usec: 456}},
					Eproc: unix.Eproc{Ppid: 565},
				}
				test.edit(info)
				return info, nil
			}
			t.Cleanup(func() {
				inspectProcBSDInfo, sysctlKinfoProc = oldInspect, oldSysctl
			})

			if _, err := InspectProcessLink(3794); err == nil {
				t.Fatal("malformed fallback metadata was accepted")
			}
		})
	}
}

func TestInspectProcessLinkPropagatesSysctlFailure(t *testing.T) {
	oldInspect, oldSysctl := inspectProcBSDInfo, sysctlKinfoProc
	inspectProcBSDInfo = func(int) (procBSDInfo, error) {
		return procBSDInfo{}, syscall.EPERM
	}
	sysctlKinfoProc = func(string, ...int) (*unix.KinfoProc, error) {
		return nil, syscall.EIO
	}
	t.Cleanup(func() {
		inspectProcBSDInfo, sysctlKinfoProc = oldInspect, oldSysctl
	})

	if _, err := InspectProcessLink(3794); !errors.Is(err, syscall.EIO) {
		t.Fatalf("InspectProcessLink error = %v", err)
	}
}

func TestInspectProcessLinkDoesNotFallbackForOtherErrors(t *testing.T) {
	oldInspect, oldSysctl := inspectProcBSDInfo, sysctlKinfoProc
	inspectProcBSDInfo = func(int) (procBSDInfo, error) {
		return procBSDInfo{}, syscall.ESRCH
	}
	sysctlKinfoProc = func(string, ...int) (*unix.KinfoProc, error) {
		t.Fatal("unexpected sysctl fallback")
		return nil, nil
	}
	t.Cleanup(func() {
		inspectProcBSDInfo, sysctlKinfoProc = oldInspect, oldSysctl
	})

	if _, err := InspectProcessLink(3794); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("InspectProcessLink error = %v", err)
	}
}
