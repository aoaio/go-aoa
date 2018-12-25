// +build linux,!appengine darwin freebsd openbsd netbsd

package term

import (
	"syscall"
	"unsafe"
)

func IsTty(fd uintptr) bool {
	var termios Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlReadTermios, uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return err == 0
}
