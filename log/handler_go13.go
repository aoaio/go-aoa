// +build !go1.4

package log

import (
	"sync/atomic"
	"unsafe"
)

type swapHandler struct {
	handler unsafe.Pointer
}

func (h *swapHandler) Log(r *Record) error {
	return h.Get().Log(r)
}

func (h *swapHandler) Get() Handler {
	return *(*Handler)(atomic.LoadPointer(&h.handler))
}

func (h *swapHandler) Swap(newHandler Handler) {
	atomic.StorePointer(&h.handler, unsafe.Pointer(&newHandler))
}
