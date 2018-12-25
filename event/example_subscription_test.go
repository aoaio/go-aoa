package event_test

import (
	"fmt"

	"github.com/Aurorachain/go-Aurora/event"
)

func ExampleNewSubscription() {

	ch := make(chan int)
	sub := event.NewSubscription(func(quit <-chan struct{}) error {
		for i := 0; i < 10; i++ {
			select {
			case ch <- i:
			case <-quit:
				fmt.Println("unsubscribed")
				return nil
			}
		}
		return nil
	})

	for i := range ch {
		fmt.Println(i)
		if i == 4 {
			sub.Unsubscribe()
			break
		}
	}

}
