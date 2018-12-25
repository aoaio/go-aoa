package filter

import (
	"testing"
	"time"
)

func TestFilters(t *testing.T) {
	fm := New()
	fm.Start()

	first := make(chan struct{})
	fm.Install(Generic{
		Str1: "hello",
		Fn: func(data interface{}) {
			first <- struct{}{}
		},
	})
	second := make(chan struct{})
	fm.Install(Generic{
		Str1: "hello1",
		Str2: "hello",
		Fn: func(data interface{}) {
			second <- struct{}{}
		},
	})

	fm.Notify(Generic{Str1: "hello"}, true)
	fm.Stop()

	select {
	case <-first:
	case <-time.After(100 * time.Millisecond):
		t.Error("matching filter timed out")
	}
	select {
	case <-second:
		t.Error("mismatching filter fired")
	case <-time.After(100 * time.Millisecond):
	}
}
