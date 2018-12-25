// +build go1.6

package debug

import "runtime/debug"

func LoudPanic(x interface{}) {
	debug.SetTraceback("all")
	panic(x)
}
