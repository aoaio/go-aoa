package core

import (
	"container/list"
	"fmt"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/event"
)

type TestManager struct {

	eventMux *event.TypeMux

	db         aoadb.Database
	txPool     *TxPool
	blockChain *BlockChain
	Blocks     []*types.Block
}

func (tm *TestManager) IsListening() bool {
	return false
}

func (tm *TestManager) IsMining() bool {
	return false
}

func (tm *TestManager) PeerCount() int {
	return 0
}

func (tm *TestManager) Peers() *list.List {
	return list.New()
}

func (tm *TestManager) BlockChain() *BlockChain {
	return tm.blockChain
}

func (tm *TestManager) TxPool() *TxPool {
	return tm.txPool
}

func (tm *TestManager) EventMux() *event.TypeMux {
	return tm.eventMux
}

func (tm *TestManager) Db() aoadb.Database {
	return tm.db
}

func NewTestManager() *TestManager {
	db, err := aoadb.NewMemDatabase()
	if err != nil {
		fmt.Println("Could not create mem-db, failing")
		return nil
	}

	testManager := &TestManager{}
	testManager.eventMux = new(event.TypeMux)
	testManager.db = db

	return testManager
}
