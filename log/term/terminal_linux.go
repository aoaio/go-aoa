package term

import "syscall"

const ioctlReadTermios = syscall.TCGETS

type Termios syscall.Termios
