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
	"container/heap"
	"context"
	"sync"
	"time"
)

const (
	//时间间隔500毫秒
	tickPeriod = 500 * time.Millisecond
	//票池大小
	bufferSize = 1024
)

//
type OnTimeOut struct {
	Callback func(ctx context.Context)
	Ctx      context.Context
}

var timerIds *AtomicInt64

func init() {
	timerIds = NewAtomicInt64(0)
}

// timerHeap is a heap-based priority queue
type timerHeapType []*timerType

func (heap timerHeapType) getIndexByID(id int64) int {
	for _, t := range heap {
		if t.id == id {
			return t.index
		}
	}
	return -1
}

func (heap timerHeapType) Len() int {
	return len(heap)
}

func (heap timerHeapType) Less(i, j int) bool {
	return heap[i].expiration.UnixNano() < heap[j].expiration.UnixNano()
}

func (heap timerHeapType) Swap(i, j int) {
	heap[i], heap[j] = heap[j], heap[i]
	heap[i].index = i
	heap[j].index = j
}

func (heap *timerHeapType) Push(x interface{}) {
	n := len(*heap)
	timer := x.(*timerType)
	timer.index = n
	*heap = append(*heap, timer)
}

func (heap *timerHeapType) Pop() interface{} {
	old := *heap
	n := len(old)
	timer := old[n-1]
	timer.index = -1
	*heap = old[0 : n-1]
	return timer
}

/* 'expiration' is the time when timer time out, if 'interval' > 0
the timer will time out periodically, 'timeout' contains the callback
to be called when times out */
type timerType struct {
	id         int64
	expiration time.Time
	interval   time.Duration
	timeout    *OnTimeOut
	index      int // for container/heap
	ctx        *context.Context
}

func newTimer(when time.Time, interv time.Duration, to *OnTimeOut) *timerType {
	return &timerType{
		id:         timerIds.GetAndIncrement(),
		expiration: when,
		interval:   interv,
		timeout:    to,
	}
}

func (t *timerType) isRepeat() bool {
	return int64(t.interval) > 0
}

// TimingWheel manages all the timed task.
type TimingWheel struct {
	timeOutChan chan *OnTimeOut
	timers      timerHeapType
	ticker      *time.Ticker
	wg          *sync.WaitGroup
	addChan     chan *timerType // add timer in loop
	cancelChan  chan int64      // cancel timer in loop
	sizeChan    chan int        // get size in loop
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewTimingWheel returns a *TimingWheel ready for use.
func NewTimingWheel(ctx context.Context) *TimingWheel {
	timingWheel := &TimingWheel{
		timeOutChan: make(chan *OnTimeOut, bufferSize),
		timers:      make(timerHeapType, 0),
		ticker:      time.NewTicker(tickPeriod),
		wg:          &sync.WaitGroup{},
		addChan:     make(chan *timerType, bufferSize),
		cancelChan:  make(chan int64, bufferSize),
		sizeChan:    make(chan int),
	}

	timingWheel.ctx, timingWheel.cancel = context.WithCancel(ctx)
	heap.Init(&timingWheel.timers)

	timingWheel.wg.Add(1)
	timingWheel.wg.Add(1)

	go func() {
		timingWheel.start()
		timingWheel.wg.Done()
	}()

	go func() {
		timingWheel.StartOnTimeOut()
		timingWheel.wg.Done()
	}()

	return timingWheel
}

// TimeOutChannel returns the timeout channel.
func (tw *TimingWheel) TimeOutChannel() chan *OnTimeOut {
	return tw.timeOutChan
}

// AddTimer adds new timed task.
func (tw *TimingWheel) AddTimer(when time.Time, interv time.Duration, to *OnTimeOut) int64 {
	if to == nil {
		return int64(-1)
	}
	timer := newTimer(when, interv, to)
	tw.addChan <- timer
	return timer.id
}

// Size returns the number of timed tasks.
func (tw *TimingWheel) Size() int {
	return <-tw.sizeChan
}

// CancelTimer cancels a timed task with specified timer ID.
func (tw *TimingWheel) CancelTimer(timerID int64) {
	tw.cancelChan <- timerID
}

// Stop stops the TimingWheel.
func (tw *TimingWheel) Stop() {
	tw.cancel()
	tw.wg.Wait()
}

func (tw *TimingWheel) getExpired() []*timerType {
	expired := make([]*timerType, 0)
	for tw.timers.Len() > 0 {
		timer := heap.Pop(&tw.timers).(*timerType)
		elapsed := time.Since(timer.expiration).Seconds()
		if elapsed > 1.0 {

		}
		if elapsed > 0.0 {
			expired = append(expired, timer)
			continue
		} else {
			heap.Push(&tw.timers, timer)
			break
		}
	}
	return expired
}

func (tw *TimingWheel) update(timers []*timerType) {
	if timers != nil {
		for _, t := range timers {
			if t.isRepeat() { // repeatable timer task
				t.expiration = t.expiration.Add(t.interval)

				if time.Since(t.expiration).Seconds() >= 10.0 {
					t.expiration = time.Now()
				}
				heap.Push(&tw.timers, t)
			}
		}
	}
}

func (tw *TimingWheel) StartOnTimeOut() {
	for {
		select {
		case onTimeOut := <-tw.timeOutChan:
			go onTimeOut.Callback(onTimeOut.Ctx)
		case <-tw.ctx.Done():
			tw.ticker.Stop()
			return
		}
	}

}
func (tw *TimingWheel) start() {
	for {
		select {
		case timerID := <-tw.cancelChan:
			index := tw.timers.getIndexByID(timerID)
			if index >= 0 {
				heap.Remove(&tw.timers, index)
			}

		case tw.sizeChan <- tw.timers.Len():

		case <-tw.ctx.Done():
			tw.ticker.Stop()
			return

		case timer := <-tw.addChan:
			heap.Push(&tw.timers, timer)

		case <-tw.ticker.C:
			timers := tw.getExpired()
			for _, t := range timers {
				tw.TimeOutChannel() <- t.timeout
			}
			tw.update(timers)
		}
	}
}
