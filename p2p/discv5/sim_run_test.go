package discv5

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func getnacl() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		_, err := exec.LookPath("sel_ldr_x86_64")
		return "amd64p32", err
	case "i386":
		_, err := exec.LookPath("sel_ldr_i386")
		return "i386", err
	default:
		return "", errors.New("nacl is not supported on " + runtime.GOARCH)
	}
}

func runWithPlaygroundTime(t *testing.T) (isHost bool) {
	if runtime.GOOS == "nacl" {
		return false
	}

	callerPC, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("can't get caller")
	}
	callerFunc := runtime.FuncForPC(callerPC)
	if callerFunc == nil {
		panic("can't get caller")
	}
	callerName := callerFunc.Name()[strings.LastIndexByte(callerFunc.Name(), '.')+1:]
	if !strings.HasPrefix(callerName, "Test") {
		panic("must be called from witin a Test* function")
	}
	testPattern := "^" + callerName + "$"

	arch, err := getnacl()
	if err != nil {
		t.Skip(err)
	}

	cmd := exec.Command("go", "test", "-v", "-tags", "faketime_simulation", "-timeout", "100h", "-run", testPattern, ".")
	cmd.Env = append([]string{"GOOS=nacl", "GOARCH=" + arch}, os.Environ()...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	go skipPlaygroundOutputHeaders(os.Stdout, stdout)
	go skipPlaygroundOutputHeaders(os.Stderr, stderr)
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}

	return true
}

func skipPlaygroundOutputHeaders(out io.Writer, in io.Reader) {

	bufin := bufio.NewReader(in)
	output, err := bufin.ReadBytes(0)
	output = bytes.TrimSuffix(output, []byte{0})
	if len(output) > 0 {
		out.Write(output)
	}
	if err != nil {
		return
	}
	bufin.UnreadByte()

	head := make([]byte, 4+8+4)
	for {
		if _, err := io.ReadFull(bufin, head); err != nil {
			if err != io.EOF {
				fmt.Fprintln(out, "read error:", err)
			}
			return
		}
		if !bytes.HasPrefix(head, []byte{0x00, 0x00, 'P', 'B'}) {
			fmt.Fprintf(out, "expected playback header, got %q\n", head)
			io.Copy(out, bufin)
			return
		}

		size := binary.BigEndian.Uint32(head[12:])
		io.CopyN(out, bufin, int64(size))
	}
}
