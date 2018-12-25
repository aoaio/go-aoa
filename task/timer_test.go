package task

import (
	"testing"

	"context"
	"fmt"
	"sync"
	"time"
)

func TestNewTimingWheel(t *testing.T) {
	tw := NewTimingWheel(context.Background())
	var wb sync.WaitGroup
	wb.Add(1)
	wb.Add(1)
	wb.Add(1)

	onTimeOut1 := &OnTimeOut{
		Callback: func(ctx context.Context) {
			fmt.Print(ctx.Value("param"))
			wb.Done()
		},
		Ctx: context.WithValue(context.Background(), "param", "1sss"),
	}
	onTimeOut2 := &OnTimeOut{
		Callback: func(ctx context.Context) {
			fmt.Print(ctx.Value("param"))
			wb.Done()
		},
		Ctx: context.WithValue(context.Background(), "param", "sss2"),
	}
	onTimeOut3 := &OnTimeOut{
		Callback: func(ctx context.Context) {
			fmt.Println(ctx.Value("param"))
			wb.Done()
		},
		Ctx: context.WithValue(context.Background(), "param", "sss3"),
	}
	onTimeOut4 := &OnTimeOut{
		Callback: func(ctx context.Context) {
			fmt.Println(ctx.Value("param"))
			wb.Done()
		},
		Ctx: context.WithValue(context.Background(), "param", "sss8"),
	}

	tw.AddTimer(time.Now().Add(time.Second),
		-1, onTimeOut1)
	tw.AddTimer(time.Now().Add(2*time.Second),
		-1, onTimeOut2)
	tw.AddTimer(time.Now().Add(time.Second),
		-1, onTimeOut3)
	tw.AddTimer(time.Now().Add(-10 * time.Second),
		-1, onTimeOut4)
	wb.Wait()
}
