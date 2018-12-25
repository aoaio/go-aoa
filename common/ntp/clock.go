package ntp

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"
)

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

func New() Clock {
	return &clock{}
}

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

type Mock struct {
	mu     sync.Mutex
	now    time.Time   
	timers clockTimers 
}

func NewMock() *Mock {
	return &Mock{now: time.Unix(0, 0)}
}

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

func checkLocalTimeIsNtp() error {
	ntpTime, err := Time(NtpHost2)
	localTime := time.Now()

	if err != nil {
		return err
	}
	localTimeNano := localTime.UnixNano()
	ntpTimeNano := ntpTime.UnixNano()

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

	if localTimeNano-ntpTimeNano > -timDif && localTimeNano-ntpTimeNano < timDif {
		return nil
	}
	errString := fmt.Sprintf("local time is not ntp time,please set first. localTimeNano:%d ntpTimeNano:%d timDif:%d ntpHost:%s", localTimeNano, ntpTimeNano, localTimeNano-ntpTimeNano, ntpHost)
	return errors.New(errString)
}

func (m *Mock) Add(d time.Duration) {

	t := m.now.Add(d)

	for {
		if !m.runNextTimer(t) {
			break
		}
	}

	m.mu.Lock()
	m.now = t
	m.mu.Unlock()

	gosched()
}

func (m *Mock) Set(t time.Time) {

	for {
		if !m.runNextTimer(t) {
			break
		}
	}

	m.mu.Lock()
	m.now = t
	m.mu.Unlock()

	gosched()
}

func (m *Mock) runNextTimer(max time.Time) bool {
	m.mu.Lock()

	sort.Sort(m.timers)

	if len(m.timers) == 0 {
		m.mu.Unlock()
		return false
	}

	t := m.timers[0]
	if t.Next().After(max) {
		m.mu.Unlock()
		return false
	}

	m.now = t.Next()
	m.mu.Unlock()

	t.Tick(m.now)
	return true
}

func (m *Mock) After(d time.Duration) <-chan time.Time {
	return m.Timer(d).C
}

func (m *Mock) AfterFunc(d time.Duration, f func()) *Timer {
	t := m.Timer(d)
	t.C = nil
	t.fn = f
	return t
}

func (m *Mock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *Mock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *Mock) Sleep(d time.Duration) {
	<-m.After(d)
}

func (m *Mock) Tick(d time.Duration) <-chan time.Time {
	return m.Ticker(d).C
}

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

type clockTimer interface {
	Next() time.Time
	Tick(time.Time)
}

type clockTimers []clockTimer

func (a clockTimers) Len() int           { return len(a) }
func (a clockTimers) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a clockTimers) Less(i, j int) bool { return a[i].Next().Before(a[j].Next()) }

type Timer struct {
	C       <-chan time.Time
	c       chan time.Time
	timer   *time.Timer 
	next    time.Time   
	mock    *Mock       
	fn      func()      
	stopped bool        
}

func (t *Timer) Stop() bool {
	if t.timer != nil {
		return t.timer.Stop()
	}

	registered := !t.stopped
	t.mock.removeClockTimer((*internalTimer)(t))
	t.stopped = true
	return registered
}

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

type Ticker struct {
	C      <-chan time.Time
	c      chan time.Time
	ticker *time.Ticker  
	next   time.Time     
	mock   *Mock         
	d      time.Duration 
}

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

func gosched() { time.Sleep(1 * time.Millisecond) }
