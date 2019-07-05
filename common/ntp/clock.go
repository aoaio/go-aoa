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

package ntp

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Clock represents an interface to the functions in the standard library time
// package. Two implementations are available in the clock package. The first
// is a real-time clock which simply wraps the time package's functions. The
// second is a mock clock which will only make forward progress when
// programmatically adjusted.
type Clock interface {
	After(d time.Duration) <-chan time.Time
	AfterFunc(d time.Duration, f func()) *Timer
	Now() time.Time
	Since(t time.Time) time.Duration
	Sleep(d time.Duration)
	Tick(d time.Duration) <-chan time.Time
	Ticker(d time.Duration) *Ticker
	Timer(d time.Duration) *Timer
}

// New returns an instance of a real-time clock.
func New() Clock {
	return &clock{}
}

// clock implements a real-time clock by simply wrapping the time package functions.
type clock struct{}

func (c *clock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (c *clock) AfterFunc(d time.Duration, f func()) *Timer {
	return &Timer{timer: time.AfterFunc(d, f)}
}

func (c *clock) Now() time.Time { return time.Now() }

func (c *clock) Since(t time.Time) time.Duration { return time.Since(t) }

func (c *clock) Sleep(d time.Duration) { time.Sleep(d) }

func (c *clock) Tick(d time.Duration) <-chan time.Time { return time.Tick(d) }

func (c *clock) Ticker(d time.Duration) *Ticker {
	t := time.NewTicker(d)
	return &Ticker{C: t.C, ticker: t}
}

func (c *clock) Timer(d time.Duration) *Timer {
	t := time.NewTimer(d)
	return &Timer{C: t.C, timer: t}
}

// Mock represents a mock clock that only moves forward programmically.
// It can be preferable to a real-time clock when testing time-based functionality.
type Mock struct {
	mu     sync.Mutex
	now    time.Time   // current time
	timers clockTimers // tickers & timers
}

// NewMock returns an instance of a mock clock.
// The current time of the mock clock on initialization is the Unix epoch.
func NewMock() *Mock {
	return &Mock{now: time.Unix(0, 0)}
}

// ntp clock
func NewNtpClock() (*Mock, error) {
	ntpTime, err := Time(NtpHost1)
	if err != nil {
		return nil, err
	}
	mock := &Mock{now: time.Unix(0, 0)}
	mock.Set(ntpTime)
	tick := time.NewTicker(500 * time.Millisecond)

	go func() {
		for {
			select {
			case <-tick.C:
				mock.Add(500 * time.Millisecond)

			}
		}

	}()
	return mock, nil
}

// try 10 times again
func CheckLocalTimeIsNtp() error {
	var ntpList = []string{NtpHost, NtpHost4, NtpHost5, NtpHost6, NtpHost2, NtpHost1, NtpHost3}

	err := checkLocalTimeNtp(NtpHost)
	for _, v := range ntpList {
		for i := 0; i < 4; i++ {
			err = checkLocalTimeNtp(v)
			if err == nil {
				return nil
			} else {
				fmt.Printf("ntp error:%v\n", err.Error())
			}
		}
	}
	return err
}

const timDif = 2500000000

// check local time is ntp
func checkLocalTimeIsNtp() error {
	ntpTime, err := Time(NtpHost2)
	localTime := time.Now()

	if err != nil {
		return err
	}
	localTimeNano := localTime.UnixNano()
	ntpTimeNano := ntpTime.UnixNano()
	// fmt.Printf("checkLocalTimeIsNtp localTime:%d  ntpTime:%d \n",localTimeNano,ntpTimeNano)
	if localTimeNano-ntpTimeNano > -timDif && localTimeNano-ntpTimeNano < timDif {
		return nil
	}
	return errors.New("local time is not ntp time,please set first")
}

func checkLocalTimeNtp(ntpHost string) error {
	if runtime.GOOS == "windows" {
		fmt.Printf("windows ignore ntp check \n")
		return nil
	}
	ntpTime, err := Time(ntpHost)
	localTime := time.Now()

	if err != nil {
		return err
	}
	localTimeNano := localTime.UnixNano()
	ntpTimeNano := ntpTime.UnixNano()
	// fmt.Printf("checkLocalTimeIsNtp localTime:%d  ntpTime:%d \n",localTimeNano,ntpTimeNano)
	if localTimeNano-ntpTimeNano > -timDif && localTimeNano-ntpTimeNano < timDif {
		return nil
	}
	errString := fmt.Sprintf("local time is not ntp time,please set first. localTimeNano:%d ntpTimeNano:%d timDif:%d ntpHost:%s", localTimeNano, ntpTimeNano, localTimeNano-ntpTimeNano, ntpHost)
	return errors.New(errString)
}

// Add moves the current time of the mock clock forward by the duration.
// This should only be called from a single goroutine at a time.
func (m *Mock) Add(d time.Duration) {
	// Calculate the final current time.
	t := m.now.Add(d)

	// Continue to execute timers until there are no more before the new time.
	for {
		if !m.runNextTimer(t) {
			break
		}
	}

	// Ensure that we end with the new time.
	m.mu.Lock()
	m.now = t
	m.mu.Unlock()

	// Give a small buffer to make sure the other goroutines get handled.
	gosched()
}

// Set sets the current time of the mock clock to a specific one.
// This should only be called from a single goroutine at a time.
func (m *Mock) Set(t time.Time) {
	// Continue to execute timers until there are no more before the new time.
	for {
		if !m.runNextTimer(t) {
			break
		}
	}

	// Ensure that we end with the new time.
	m.mu.Lock()
	m.now = t
	m.mu.Unlock()

	// Give a small buffer to make sure the other goroutines get handled.
	gosched()
}

// runNextTimer executes the next timer in chronological order and moves the
// current time to the timer's next tick time. The next time is not executed if
// it's next time if after the max time. Returns true if a timer is executed.
func (m *Mock) runNextTimer(max time.Time) bool {
	m.mu.Lock()

	// Sort timers by time.
	sort.Sort(m.timers)

	// If we have no more timers then exit.
	if len(m.timers) == 0 {
		m.mu.Unlock()
		return false
	}

	// Retrieve next timer. Exit if next tick is after new time.
	t := m.timers[0]
	if t.Next().After(max) {
		m.mu.Unlock()
		return false
	}

	// Move "now" forward and unlock clock.
	m.now = t.Next()
	m.mu.Unlock()

	// Execute timer.
	t.Tick(m.now)
	return true
}

// After waits for the duration to elapse and then sends the current time on the returned channel.
func (m *Mock) After(d time.Duration) <-chan time.Time {
	return m.Timer(d).C
}

// AfterFunc waits for the duration to elapse and then executes a function.
// A Timer is returned that can be stopped.
func (m *Mock) AfterFunc(d time.Duration, f func()) *Timer {
	t := m.Timer(d)
	t.C = nil
	t.fn = f
	return t
}

// Now returns the current wall time on the mock clock.
func (m *Mock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Since returns time since the mock clocks wall time.
func (m *Mock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

// Sleep pauses the goroutine for the given duration on the mock clock.
// The clock must be moved forward in a separate goroutine.
func (m *Mock) Sleep(d time.Duration) {
	<-m.After(d)
}

// Tick is a convenience function for Ticker().
// It will return a ticker channel that cannot be stopped.
func (m *Mock) Tick(d time.Duration) <-chan time.Time {
	return m.Ticker(d).C
}

// Ticker creates a new instance of Ticker.
func (m *Mock) Ticker(d time.Duration) *Ticker {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan time.Time, 1)
	t := &Ticker{
		C:    ch,
		c:    ch,
		mock: m,
		d:    d,
		next: m.now.Add(d),
	}
	m.timers = append(m.timers, (*internalTicker)(t))
	return t
}

// Timer creates a new instance of Timer.
func (m *Mock) Timer(d time.Duration) *Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan time.Time, 1)
	t := &Timer{
		C:       ch,
		c:       ch,
		mock:    m,
		next:    m.now.Add(d),
		stopped: false,
	}
	m.timers = append(m.timers, (*internalTimer)(t))
	return t
}

func (m *Mock) removeClockTimer(t clockTimer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, timer := range m.timers {
		if timer == t {
			copy(m.timers[i:], m.timers[i+1:])
			m.timers[len(m.timers)-1] = nil
			m.timers = m.timers[:len(m.timers)-1]
			break
		}
	}
	sort.Sort(m.timers)
}

// clockTimer represents an object with an associated start time.
type clockTimer interface {
	Next() time.Time
	Tick(time.Time)
}

// clockTimers represents a list of sortable timers.
type clockTimers []clockTimer

func (a clockTimers) Len() int           { return len(a) }
func (a clockTimers) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a clockTimers) Less(i, j int) bool { return a[i].Next().Before(a[j].Next()) }

// Timer represents a single event.
// The current time will be sent on C, unless the timer was created by AfterFunc.
type Timer struct {
	C       <-chan time.Time
	c       chan time.Time
	timer   *time.Timer // realtime impl, if set
	next    time.Time   // next tick time
	mock    *Mock       // mock clock, if set
	fn      func()      // AfterFunc function, if set
	stopped bool        // True if stopped, false if running
}

// Stop turns off the ticker.
func (t *Timer) Stop() bool {
	if t.timer != nil {
		return t.timer.Stop()
	}

	registered := !t.stopped
	t.mock.removeClockTimer((*internalTimer)(t))
	t.stopped = true
	return registered
}

// Reset changes the expiry time of the timer
func (t *Timer) Reset(d time.Duration) bool {
	if t.timer != nil {
		return t.timer.Reset(d)
	}

	t.next = t.mock.now.Add(d)
	registered := !t.stopped
	if t.stopped {
		t.mock.mu.Lock()
		t.mock.timers = append(t.mock.timers, (*internalTimer)(t))
		t.mock.mu.Unlock()
	}
	t.stopped = false
	return registered
}

type internalTimer Timer

func (t *internalTimer) Next() time.Time { return t.next }
func (t *internalTimer) Tick(now time.Time) {
	if t.fn != nil {
		t.fn()
	} else {
		t.c <- now
	}
	t.mock.removeClockTimer((*internalTimer)(t))
	t.stopped = true
	gosched()
}

// Ticker holds a channel that receives "ticks" at regular intervals.
type Ticker struct {
	C      <-chan time.Time
	c      chan time.Time
	ticker *time.Ticker  // realtime impl, if set
	next   time.Time     // next tick time
	mock   *Mock         // mock clock, if set
	d      time.Duration // time between ticks
}

// Stop turns off the ticker.
func (t *Ticker) Stop() {
	if t.ticker != nil {
		t.ticker.Stop()
	} else {
		t.mock.removeClockTimer((*internalTicker)(t))
	}
}

type internalTicker Ticker

func (t *internalTicker) Next() time.Time { return t.next }
func (t *internalTicker) Tick(now time.Time) {
	select {
	case t.c <- now:
	default:
	}
	t.next = now.Add(t.d)
	gosched()
}

// Sleep momentarily so that other goroutines can process.
func gosched() { time.Sleep(1 * time.Millisecond) }
