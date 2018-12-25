package rpc

import (
	"context"
	"errors"
	"sync"
)

var (

	ErrNotificationsUnsupported = errors.New("notifications not supported")

	ErrSubscriptionNotFound = errors.New("subscription not found")
)

type ID string

type Subscription struct {
	ID        ID
	namespace string
	err       chan error
}

func (s *Subscription) Err() <-chan error {
	return s.err
}

type notifierKey struct{}

type Notifier struct {
	codec    ServerCodec
	subMu    sync.RWMutex
	active   map[ID]*Subscription
	inactive map[ID]*Subscription
}

func newNotifier(codec ServerCodec) *Notifier {
	return &Notifier{
		codec:    codec,
		active:   make(map[ID]*Subscription),
		inactive: make(map[ID]*Subscription),
	}
}

func NotifierFromContext(ctx context.Context) (*Notifier, bool) {
	n, ok := ctx.Value(notifierKey{}).(*Notifier)
	return n, ok
}

func (n *Notifier) CreateSubscription() *Subscription {
	s := &Subscription{ID: NewID(), err: make(chan error)}
	n.subMu.Lock()
	n.inactive[s.ID] = s
	n.subMu.Unlock()
	return s
}

func (n *Notifier) Notify(id ID, data interface{}) error {
	n.subMu.RLock()
	defer n.subMu.RUnlock()

	sub, active := n.active[id]
	if active {
		notification := n.codec.CreateNotification(string(id), sub.namespace, data)
		if err := n.codec.Write(notification); err != nil {
			n.codec.Close()
			return err
		}
	}
	return nil
}

func (n *Notifier) Closed() <-chan interface{} {
	return n.codec.Closed()
}

func (n *Notifier) unsubscribe(id ID) error {
	n.subMu.Lock()
	defer n.subMu.Unlock()
	if s, found := n.active[id]; found {
		close(s.err)
		delete(n.active, id)
		return nil
	}
	return ErrSubscriptionNotFound
}

func (n *Notifier) activate(id ID, namespace string) {
	n.subMu.Lock()
	defer n.subMu.Unlock()
	if sub, found := n.inactive[id]; found {
		sub.namespace = namespace
		n.active[id] = sub
		delete(n.inactive, id)
	}
}
