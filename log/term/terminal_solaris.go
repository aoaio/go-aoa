package term

import "golang.org/x/sys/unix"

func IsTty(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TCGETA)
	return err == nil
}
