package discv5

import (
	"os"
	"runtime"
	"testing"
	"unsafe"
)

var faketime = 1

func TestMain(m *testing.M) {

	_ = unsafe.Sizeof(0)

	runtime.GOMAXPROCS(8)
	os.Exit(m.Run())
}
