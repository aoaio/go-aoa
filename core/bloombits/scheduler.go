package bloombits

import (
	"sync"
)

type request struct {
	section uint64
	bit     uint
}

type response struct {
	cached []byte
	done   chan struct{}
}

type scheduler struct {
	bit       uint
	responses map[uint64]*response
	lock      sync.Mutex
}

func newScheduler(idx uint) *scheduler {
	return &scheduler{
		bit:       idx,
		responses: make(map[uint64]*response),
	}
}

func (s *scheduler) run(sections chan uint64, dist chan *request, done chan []byte, quit chan struct{}, wg *sync.WaitGroup) {

	pend := make(chan uint64, cap(dist))

	wg.Add(2)
	go s.scheduleRequests(sections, dist, pend, quit, wg)
	go s.scheduleDeliveries(pend, done, quit, wg)
}

func (s *scheduler) reset() {
	s.lock.Lock()
	defer s.lock.Unlock()

	for section, res := range s.responses {
		if res.cached == nil {
			delete(s.responses, section)
		}
	}
}

func (s *scheduler) scheduleRequests(reqs chan uint64, dist chan *request, pend chan uint64, quit chan struct{}, wg *sync.WaitGroup) {

	defer wg.Done()
	defer close(pend)

	for {
		select {
		case <-quit:
			return

		case section, ok := <-reqs:

			if !ok {
				return
			}

			unique := false

			s.lock.Lock()
			if s.responses[section] == nil {
				s.responses[section] = &response{
					done: make(chan struct{}),
				}
				unique = true
			}
			s.lock.Unlock()

			if unique {
				select {
				case <-quit:
					return
				case dist <- &request{bit: s.bit, section: section}:
				}
			}
			select {
			case <-quit:
				return
			case pend <- section:
			}
		}
	}
}

func (s *scheduler) scheduleDeliveries(pend chan uint64, done chan []byte, quit chan struct{}, wg *sync.WaitGroup) {

	defer wg.Done()
	defer close(done)

	for {
		select {
		case <-quit:
			return

		case idx, ok := <-pend:

			if !ok {
				return
			}

			s.lock.Lock()
			res := s.responses[idx]
			s.lock.Unlock()

			select {
			case <-quit:
				return
			case <-res.done:
			}

			select {
			case <-quit:
				return
			case done <- res.cached:
			}
		}
	}
}

func (s *scheduler) deliver(sections []uint64, data [][]byte) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for i, section := range sections {
		if res := s.responses[section]; res != nil && res.cached == nil {
			res.cached = data[i]
			close(res.done)
		}
	}
}
