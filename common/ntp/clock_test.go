package ntp

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClock_After(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	<-New().After(20 * time.Millisecond)
	if !ok {
		t.Fatal("too early")
	}
}

func TestClock_AfterFunc(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	var wg sync.WaitGroup
	wg.Add(1)
	New().AfterFunc(20*time.Millisecond, func() {
		wg.Done()
	})
	wg.Wait()
	if !ok {
		t.Fatal("too early")
	}
}

func TestClock_Now(t *testing.T) {
	a := time.Now().Round(time.Second)
	b := New().Now().Round(time.Second)
	if !a.Equal(b) {
		t.Errorf("not equal: %s != %s", a, b)
	}
}

func TestClock_Sleep(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	New().Sleep(20 * time.Millisecond)
	if !ok {
		t.Fatal("too early")
	}
}

func TestClock_Tick(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(50 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	c := New().Tick(20 * time.Millisecond)
	<-c
	<-c
	if !ok {
		t.Fatal("too early")
	}
}

func TestClock_Ticker(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(100 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	ticker := New().Ticker(50 * time.Millisecond)
	<-ticker.C
	<-ticker.C
	if !ok {
		t.Fatal("too early")
	}
}

func TestClock_Ticker_Stp(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	gosched()

	ticker := New().Ticker(20 * time.Millisecond)
	<-ticker.C
	ticker.Stop()
	select {
	case <-ticker.C:
		t.Fatal("unexpected send")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestClock_Timer(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	timer := New().Timer(20 * time.Millisecond)
	<-timer.C
	if !ok {
		t.Fatal("too early")
	}

	if timer.Stop() {
		t.Fatal("timer still running")
	}
}

func TestClock_Timer_Stop(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok = true
	}()

	timer := New().Timer(20 * time.Millisecond)
	if !timer.Stop() {
		t.Fatal("timer not running")
	}
	if timer.Stop() {
		t.Fatal("timer wasn't cancelled")
	}
	select {
	case <-timer.C:
		t.Fatal("unexpected send")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestClock_Timer_Reset(t *testing.T) {
	var ok bool
	go func() {
		time.Sleep(20 * time.Millisecond)
		ok = true
	}()
	go func() {
		time.Sleep(30 * time.Millisecond)
		t.Fatal("too late")
	}()
	gosched()

	timer := New().Timer(10 * time.Millisecond)
	if !timer.Reset(20 * time.Millisecond) {
		t.Fatal("timer not running")
	}

	<-timer.C
	if !ok {
		t.Fatal("too early")
	}
}

func TestMock_After(t *testing.T) {
	var ok int32
	clock := NewMock()

	ch := clock.After(10 * time.Second)
	go func(ch <-chan time.Time) {
		<-ch
		atomic.StoreInt32(&ok, 1)
	}(ch)

	clock.Add(9 * time.Second)
	if atomic.LoadInt32(&ok) == 1 {
		t.Fatal("too early")
	}

	clock.Add(1 * time.Second)
	if atomic.LoadInt32(&ok) == 0 {
		t.Fatal("too late")
	}
}

func TestMock_UnusedAfter(t *testing.T) {
	mock := NewMock()
	mock.After(1 * time.Millisecond)

	done := make(chan bool, 1)
	go func() {
		mock.Add(1 * time.Second)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("mock.Add hung")
	}
}

func TestMock_AfterFunc(t *testing.T) {
	var ok int32
	clock := NewMock()

	clock.AfterFunc(10*time.Second, func() {
		atomic.StoreInt32(&ok, 1)
	})

	clock.Add(9 * time.Second)
	if atomic.LoadInt32(&ok) == 1 {
		t.Fatal("too early")
	}

	clock.Add(1 * time.Second)
	if atomic.LoadInt32(&ok) == 0 {
		t.Fatal("too late")
	}
}

func TestMock_AfterFunc_Stop(t *testing.T) {

	clock := NewMock()
	timer := clock.AfterFunc(10*time.Second, func() {
		t.Fatal("unexpected function execution")
	})
	gosched()

	timer.Stop()
	clock.Add(10 * time.Second)
	gosched()
}

func TestMock_Now(t *testing.T) {
	begin := time.Now()
	var n int64

	clock := NewMock()
	clock.Set(time.Now())

	tick := time.NewTicker(1 * time.Second)

	go func() {
		for {
			select {
			case <-tick.C:
				n++
				start := time.Now()
				clock.Set(time.Unix(begin.Unix()+n, 0))
				end := time.Now()
				t.Log(end.Unix() - start.Unix())
				t.Log("111")

			}
		}

	}()
	t.Logf("clock now:%v\n", clock.now)
	t.Logf("local now:%v\n", time.Now())

	time.Sleep(6 * time.Second)

	t.Logf("clock now:%v\n", clock.now)
	t.Logf("local now:%v\n", time.Now())

}

func TestNewNtpClock(t *testing.T) {

	clock, err := NewNtpClock()
	if err != nil {
		t.Fatalf("failed to init ntp time:%s\n", err)
	}

	t.Logf("clock now:%v\n", clock.now)
	t.Logf("local now:%v\n", time.Now())

	time.Sleep(6 * time.Second)

	t.Logf("clock now:%v\n", clock.now)
	t.Logf("local now:%v\n", time.Now())
}

func TestMock_Since(t *testing.T) {
	clock := NewMock()

	beginning := clock.Now()
	clock.Add(500 * time.Second)
	if since := clock.Since(beginning); since.Seconds() != 500 {
		t.Fatalf("expected 500 since beginning, actually: %v", since.Seconds())
	}
}

func TestMock_Sleep(t *testing.T) {
	var ok int32
	clock := NewMock()

	go func() {
		clock.Sleep(10 * time.Second)
		atomic.StoreInt32(&ok, 1)
	}()
	gosched()

	clock.Add(9 * time.Second)
	if atomic.LoadInt32(&ok) == 1 {
		t.Fatal("too early")
	}

	clock.Add(1 * time.Second)
	if atomic.LoadInt32(&ok) == 0 {
		t.Fatal("too late")
	}
}

func TestMock_Tick(t *testing.T) {
	var n int32
	clock := NewMock()

	go func() {
		tick := clock.Tick(10 * time.Second)
		for {
			<-tick
			atomic.AddInt32(&n, 1)
		}
	}()
	gosched()

	clock.Add(9 * time.Second)
	if atomic.LoadInt32(&n) != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	clock.Add(1 * time.Second)
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	clock.Add(30 * time.Second)
	if atomic.LoadInt32(&n) != 4 {
		t.Fatalf("expected 4, got %d", n)
	}
}

func TestMock_Ticker(t *testing.T) {
	var n int32
	clock := NewMock()

	go func() {
		ticker := clock.Ticker(1 * time.Microsecond)
		for {
			<-ticker.C
			atomic.AddInt32(&n, 1)
		}
	}()
	gosched()

	clock.Add(10 * time.Microsecond)
	if atomic.LoadInt32(&n) != 10 {
		t.Fatalf("unexpected: %d", n)
	}
}

func TestMock_Ticker_Overflow(t *testing.T) {
	clock := NewMock()
	ticker := clock.Ticker(1 * time.Microsecond)
	clock.Add(10 * time.Microsecond)
	ticker.Stop()
}

func TestMock_Ticker_Stop(t *testing.T) {
	var n int32
	clock := NewMock()

	ticker := clock.Ticker(1 * time.Second)
	go func() {
		for {
			<-ticker.C
			atomic.AddInt32(&n, 1)
		}
	}()
	gosched()

	clock.Add(5 * time.Second)
	if atomic.LoadInt32(&n) != 5 {
		t.Fatalf("expected 5, got: %d", n)
	}

	ticker.Stop()

	clock.Add(5 * time.Second)
	if atomic.LoadInt32(&n) != 5 {
		t.Fatalf("still expected 5, got: %d", n)
	}
}

func TestMock_Ticker_Multi(t *testing.T) {
	var n int32
	clock := NewMock()

	go func() {
		a := clock.Ticker(1 * time.Microsecond)
		b := clock.Ticker(3 * time.Microsecond)

		for {
			select {
			case <-a.C:
				atomic.AddInt32(&n, 1)
			case <-b.C:
				atomic.AddInt32(&n, 100)
			}
		}
	}()
	gosched()

	clock.Add(10 * time.Microsecond)
	gosched()
	if atomic.LoadInt32(&n) != 310 {
		t.Fatalf("unexpected: %d", n)
	}
}

func ExampleMock_After() {

	clock := NewMock()
	count := 0

	ready := make(chan struct{})

	go func() {
		ch := clock.After(10 * time.Second)
		close(ready)
		<-ch
		count = 100
	}()
	<-ready

	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

	clock.Add(5 * time.Second)
	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

	clock.Add(5 * time.Second)
	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

}

func ExampleMock_AfterFunc() {

	clock := NewMock()
	count := 0

	clock.AfterFunc(10*time.Second, func() {
		count = 100
	})
	gosched()

	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

	clock.Add(10 * time.Second)
	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

}

func ExampleMock_Sleep() {

	clock := NewMock()
	count := 0

	go func() {
		clock.Sleep(10 * time.Second)
		count = 100
	}()
	gosched()

	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

	clock.Add(10 * time.Second)
	fmt.Printf("%s: %d\n", clock.Now().UTC(), count)

}

func ExampleMock_Ticker() {

	clock := NewMock()
	count := 0

	ready := make(chan struct{})

	go func() {
		ticker := clock.Ticker(1 * time.Second)
		close(ready)
		for {
			<-ticker.C
			count++
		}
	}()
	<-ready

	clock.Add(10 * time.Second)
	fmt.Printf("Count is %d after 10 seconds\n", count)

	clock.Add(5 * time.Second)
	fmt.Printf("Count is %d after 15 seconds\n", count)

}

func ExampleMock_Timer() {

	clock := NewMock()
	count := 0

	ready := make(chan struct{})

	go func() {
		timer := clock.Timer(1 * time.Second)
		close(ready)
		<-timer.C
		count++
	}()
	<-ready

	clock.Add(10 * time.Second)
	fmt.Printf("Count is %d after 10 seconds\n", count)

}

func TestCheckLocalTimeIsNtp(t *testing.T) {

	err := CheckLocalTimeIsNtp()
	if err != nil {
		t.Fatalf("ntp error:%s", err)
	}
}

func TestClock_Since(t *testing.T) {
	ntpTime, err := Time(NtpHost)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(ntpTime)
}

func warn(v ...interface{})              { fmt.Fprintln(os.Stderr, v...) }
func warnf(msg string, v ...interface{}) { fmt.Fprintf(os.Stderr, msg+"\n", v...) }
