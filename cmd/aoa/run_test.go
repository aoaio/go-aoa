package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/docker/docker/pkg/reexec"
	"github.com/Aurorachain/go-Aurora/internal/cmdtest"
	"strings"
)

func tmpdir(t *testing.T) string {
	dir, err := ioutil.TempDir("", "aoa-test")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

type testgeth struct {
	*cmdtest.TestCmd

	Datadir   string
	Etherbase string
}

func init() {

	reexec.Register("aoa-test", func() {
		if err := app.Run(os.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	})
}

func TestMain(m *testing.M) {

	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}

func runGeth(t *testing.T, args ...string) *testgeth {
	tt := &testgeth{}
	tt.TestCmd = cmdtest.NewTestCmd(t, tt)
	for i, arg := range args {
		switch {
		case arg == "-datadir" || arg == "--datadir":
			if i < len(args)-1 {
				tt.Datadir = args[i+1]
			}
		case arg == "-etherbase" || arg == "--etherbase":
			if i < len(args)-1 {
				tt.Etherbase = args[i+1]
			}
		}
	}
	if tt.Datadir == "" {
		tt.Datadir = tmpdir(t)
		tt.Cleanup = func() { os.RemoveAll(tt.Datadir) }
		args = append([]string{"-datadir", tt.Datadir}, args...)

		defer func() {
			if t.Failed() {
				tt.Cleanup()
			}
		}()
	}

	tt.Run("aoa-test", args...)

	return tt
}

func TestByCategory_Len(t *testing.T) {
	s := "   ahuidhiuahiuahidun   oawd   joiadaojo      "
	ts := strings.TrimFunc(s, isSpace)
	fmt.Println(ts)
}

func isSpace(r rune) bool {
	return r == ' '
}

func TestByCategory_Less(t *testing.T) {
	s := "AOA5ccb115ac633e2ccfebe65fc4e18e75ce78642b4QWERTYUIOPASDFGHJKLZXCVBNMQWERTY"
	x := s[:(len(s) - 32)] + strings.ToLower(s[(len(s) - 32):])
	fmt.Println(x)
}