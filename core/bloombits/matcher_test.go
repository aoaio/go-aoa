package bloombits

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aurorachain/go-Aurora/common"
)

const testSectionSize = 4096

func TestMatcherWildcards(t *testing.T) {
	matcher := NewMatcher(testSectionSize, [][][]byte{
		{common.Address{}.Bytes(), common.Address{0x01}.Bytes()},
		{common.Hash{}.Bytes(), common.Hash{0x01}.Bytes()},
		{common.Hash{0x01}.Bytes()},
		{common.Hash{0x01}.Bytes(), nil},
		{nil, common.Hash{0x01}.Bytes()},
		{nil, nil},
		{},
		nil,
	})
	if len(matcher.filters) != 3 {
		t.Fatalf("filter system size mismatch: have %d, want %d", len(matcher.filters), 3)
	}
	if len(matcher.filters[0]) != 2 {
		t.Fatalf("address clause size mismatch: have %d, want %d", len(matcher.filters[0]), 2)
	}
	if len(matcher.filters[1]) != 2 {
		t.Fatalf("combo topic clause size mismatch: have %d, want %d", len(matcher.filters[1]), 2)
	}
	if len(matcher.filters[2]) != 1 {
		t.Fatalf("singletone topic clause size mismatch: have %d, want %d", len(matcher.filters[2]), 1)
	}
}

func TestMatcherContinuous(t *testing.T) {
	testMatcherDiffBatches(t, [][]bloomIndexes{{{10, 20, 30}}}, 0, 100000, false, 75)
	testMatcherDiffBatches(t, [][]bloomIndexes{{{32, 3125, 100}}, {{40, 50, 10}}}, 0, 100000, false, 81)
	testMatcherDiffBatches(t, [][]bloomIndexes{{{4, 8, 11}, {7, 8, 17}}, {{9, 9, 12}, {15, 20, 13}}, {{18, 15, 15}, {12, 10, 4}}}, 0, 10000, false, 36)
}

func TestMatcherIntermittent(t *testing.T) {
	testMatcherDiffBatches(t, [][]bloomIndexes{{{10, 20, 30}}}, 0, 100000, true, 75)
	testMatcherDiffBatches(t, [][]bloomIndexes{{{32, 3125, 100}}, {{40, 50, 10}}}, 0, 100000, true, 81)
	testMatcherDiffBatches(t, [][]bloomIndexes{{{4, 8, 11}, {7, 8, 17}}, {{9, 9, 12}, {15, 20, 13}}, {{18, 15, 15}, {12, 10, 4}}}, 0, 10000, true, 36)
}

func TestMatcherRandom(t *testing.T) {
	for i := 0; i < 10; i++ {
		testMatcherBothModes(t, makeRandomIndexes([]int{1}, 50), 0, 10000, 0)
		testMatcherBothModes(t, makeRandomIndexes([]int{3}, 50), 0, 10000, 0)
		testMatcherBothModes(t, makeRandomIndexes([]int{2, 2, 2}, 20), 0, 10000, 0)
		testMatcherBothModes(t, makeRandomIndexes([]int{5, 5, 5}, 50), 0, 10000, 0)
		testMatcherBothModes(t, makeRandomIndexes([]int{4, 4, 4}, 20), 0, 10000, 0)
	}
}

func TestMatcherShifted(t *testing.T) {

	testMatcherBothModes(t, [][]bloomIndexes{{{16, 16, 16}}}, 9, 64, 0)
}

func TestWildcardMatcher(t *testing.T) {
	testMatcherBothModes(t, nil, 0, 10000, 0)
}

func makeRandomIndexes(lengths []int, max int) [][]bloomIndexes {
	res := make([][]bloomIndexes, len(lengths))
	for i, topics := range lengths {
		res[i] = make([]bloomIndexes, topics)
		for j := 0; j < topics; j++ {
			for k := 0; k < len(res[i][j]); k++ {
				res[i][j][k] = uint(rand.Intn(max-1) + 2)
			}
		}
	}
	return res
}

func testMatcherDiffBatches(t *testing.T, filter [][]bloomIndexes, start, blocks uint64, intermittent bool, retrievals uint32) {
	singleton := testMatcher(t, filter, start, blocks, intermittent, retrievals, 1)
	batched := testMatcher(t, filter, start, blocks, intermittent, retrievals, 16)

	if singleton != batched {
		t.Errorf("filter = %v blocks = %v intermittent = %v: request count mismatch, %v in signleton vs. %v in batched mode", filter, blocks, intermittent, singleton, batched)
	}
}

func testMatcherBothModes(t *testing.T, filter [][]bloomIndexes, start, blocks uint64, retrievals uint32) {
	continuous := testMatcher(t, filter, start, blocks, false, retrievals, 16)
	intermittent := testMatcher(t, filter, start, blocks, true, retrievals, 16)

	if continuous != intermittent {
		t.Errorf("filter = %v blocks = %v: request count mismatch, %v in continuous vs. %v in intermittent mode", filter, blocks, continuous, intermittent)
	}
}

func testMatcher(t *testing.T, filter [][]bloomIndexes, start, blocks uint64, intermittent bool, retrievals uint32, maxReqCount int) uint32 {

	matcher := NewMatcher(testSectionSize, nil)
	matcher.filters = filter

	for _, rule := range filter {
		for _, topic := range rule {
			for _, bit := range topic {
				matcher.addScheduler(bit)
			}
		}
	}

	var requested uint32

	quit := make(chan struct{})
	matches := make(chan uint64, 16)

	session, err := matcher.Start(context.Background(), start, blocks-1, matches)
	if err != nil {
		t.Fatalf("failed to stat matcher session: %v", err)
	}
	startRetrievers(session, quit, &requested, maxReqCount)

	for i := start; i < blocks; i++ {
		if expMatch3(filter, i) {
			match, ok := <-matches
			if !ok {
				t.Errorf("filter = %v  blocks = %v  intermittent = %v: expected #%v, results channel closed", filter, blocks, intermittent, i)
				return 0
			}
			if match != i {
				t.Errorf("filter = %v  blocks = %v  intermittent = %v: expected #%v, got #%v", filter, blocks, intermittent, i, match)
			}

			if intermittent {
				session.Close()
				close(quit)

				quit = make(chan struct{})
				matches = make(chan uint64, 16)

				session, err = matcher.Start(context.Background(), i+1, blocks-1, matches)
				if err != nil {
					t.Fatalf("failed to stat matcher session: %v", err)
				}
				startRetrievers(session, quit, &requested, maxReqCount)
			}
		}
	}

	match, ok := <-matches
	if ok {
		t.Errorf("filter = %v  blocks = %v  intermittent = %v: expected closed channel, got #%v", filter, blocks, intermittent, match)
	}

	session.Close()
	close(quit)

	if retrievals != 0 && requested != retrievals {
		t.Errorf("filter = %v  blocks = %v  intermittent = %v: request count mismatch, have #%v, want #%v", filter, blocks, intermittent, requested, retrievals)
	}
	return requested
}

func startRetrievers(session *MatcherSession, quit chan struct{}, retrievals *uint32, batch int) {
	requests := make(chan chan *Retrieval)

	for i := 0; i < 10; i++ {

		go session.Multiplex(batch, 100*time.Microsecond, requests)

		go func() {
			for {

				select {
				case <-quit:
					return

				case request := <-requests:
					task := <-request

					task.Bitsets = make([][]byte, len(task.Sections))
					for i, section := range task.Sections {
						if rand.Int()%4 != 0 {
							task.Bitsets[i] = generateBitset(task.Bit, section)
							atomic.AddUint32(retrievals, 1)
						}
					}
					request <- task
				}
			}
		}()
	}
}

func generateBitset(bit uint, section uint64) []byte {
	bitset := make([]byte, testSectionSize/8)
	for i := 0; i < len(bitset); i++ {
		for b := 0; b < 8; b++ {
			blockIdx := section*testSectionSize + uint64(i*8+b)
			bitset[i] += bitset[i]
			if (blockIdx % uint64(bit)) == 0 {
				bitset[i]++
			}
		}
	}
	return bitset
}

func expMatch1(filter bloomIndexes, i uint64) bool {
	for _, ii := range filter {
		if (i % uint64(ii)) != 0 {
			return false
		}
	}
	return true
}

func expMatch2(filter []bloomIndexes, i uint64) bool {
	for _, ii := range filter {
		if expMatch1(ii, i) {
			return true
		}
	}
	return false
}

func expMatch3(filter [][]bloomIndexes, i uint64) bool {
	for _, ii := range filter {
		if !expMatch2(ii, i) {
			return false
		}
	}
	return true
}
