package rpc_test

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Aurorachain/go-Aurora/rpc"
)

type Block struct {
	Number *big.Int
}

func ExampleClientSubscription() {

	client, _ := rpc.Dial("ws://127.0.0.1:8485")
	subch := make(chan Block)

	go func() {
		for i := 0; ; i++ {
			if i > 0 {
				time.Sleep(2 * time.Second)
			}
			subscribeBlocks(client, subch)
		}
	}()

	for block := range subch {
		fmt.Println("latest block:", block.Number)
	}
}

func subscribeBlocks(client *rpc.Client, subch chan Block) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sub, err := client.EthSubscribe(ctx, subch, "newBlocks")
	if err != nil {
		fmt.Println("subscribe error:", err)
		return
	}

	var lastBlock Block
	if err := client.CallContext(ctx, &lastBlock, "eth_getBlockByNumber", "latest"); err != nil {
		fmt.Println("can't get latest block:", err)
		return
	}
	subch <- lastBlock

	fmt.Println("connection lost: ", <-sub.Err())
}
