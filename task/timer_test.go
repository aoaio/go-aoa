// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

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
