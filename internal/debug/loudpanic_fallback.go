// +build !go1.6

package debug

func LoudPanic(x interface{}) {
	panic(x)
}
