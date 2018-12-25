package task

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

const (

	tickPeriod = 500 * time.Millisecond

	bufferSize = 1024
)

type OnTimeOut struct {
	Callback func(ctx context.Context)
	Ctx      context.Context
}

var timerIds *AtomicInt64

func init() {
	timerIds = NewAtomicInt64(0)
}

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
	index      int
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

type TimingWheel struct {
	timeOutChan chan *OnTimeOut
	timers      timerHeapType
	ticker      *time.Ticker
	wg          *sync.WaitGroup
	addChan     chan *timerType
	cancelChan  chan int64
	sizeChan    chan int
	ctx         context.Context
	cancel      context.CancelFunc
}

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

func (tw *TimingWheel) TimeOutChannel() chan *OnTimeOut {
	return tw.timeOutChan
}

func (tw *TimingWheel) AddTimer(when time.Time, interv time.Duration, to *OnTimeOut) int64 {
	if to == nil {
		return int64(-1)
	}
	timer := newTimer(when, interv, to)
	tw.addChan <- timer
	return timer.id
}

func (tw *TimingWheel) Size() int {
	return <-tw.sizeChan
}

func (tw *TimingWheel) CancelTimer(timerID int64) {
	tw.cancelChan <- timerID
}

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
			if t.isRepeat() {
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
