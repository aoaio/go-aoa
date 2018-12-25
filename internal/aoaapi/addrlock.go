package aoaapi

import (
	"sync"

	"github.com/Aurorachain/go-Aurora/common"
)

type AddrLocker struct {
	mu    sync.Mutex
	locks map[common.Address]*sync.Mutex
}

func (l *AddrLocker) lock(address common.Address) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[common.Address]*sync.Mutex)
	}
	if _, ok := l.locks[address]; !ok {
		l.locks[address] = new(sync.Mutex)
	}
	return l.locks[address]
}

func (l *AddrLocker) LockAddr(address common.Address) {
	l.lock(address).Lock()
}

func (l *AddrLocker) UnlockAddr(address common.Address) {
	l.lock(address).Unlock()
}
