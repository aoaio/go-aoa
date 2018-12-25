package event_test

import (
	"fmt"

	"github.com/Aurorachain/go-Aurora/event"
)

func ExampleFeed_acknowledgedEvents() {

	var feed event.Feed
	type ackedEvent struct {
		i   int
		ack chan<- struct{}
	}

	done := make(chan struct{})
	defer close(done)
	for i := 0; i < 3; i++ {
		ch := make(chan ackedEvent, 100)
		sub := feed.Subscribe(ch)
		go func() {
			defer sub.Unsubscribe()
			for {
				select {
				case ev := <-ch:
					fmt.Println(ev.i)
					ev.ack <- struct{}{}
				case <-done:
					return
				}
			}
		}()
	}

	for i := 0; i < 3; i++ {
		acksignal := make(chan struct{})
		n := feed.Send(ackedEvent{i, acksignal})
		for ack := 0; ack < n; ack++ {
			<-acksignal
		}
	}

}
