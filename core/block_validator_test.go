package core

import (
	"time"
	"testing"
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/consensus/dpos"
	"github.com/Aurorachain/go-Aurora/core/types"
	"runtime"
	"github.com/Aurorachain/go-Aurora/core/vm"
)

func TestHeaderVerification(t *testing.T) {

	var (
		testdb, _ = aoadb.NewMemDatabase()
		gspec     = &Genesis{Config: params.TestChainConfig}
		genesis   = gspec.MustCommit(testdb)
		blocks, _ = GenerateChain(params.TestChainConfig, genesis, dpos.New(), testdb, 8, nil)
	)
	headers := make([]*types.Header, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}

	chain, _ := NewBlockChain(testdb, params.TestChainConfig, dpos.New(), vm.Config{},nil)
	defer chain.Stop()

	for i := 0; i < len(blocks); i++ {
		for j, valid := range []bool{true, false} {
			var results <-chan error

			engine := dpos.New()
			_, results = engine.VerifyHeaders(chain, []*types.Header{headers[i]})

			select {
			case result := <-results:
				if (result == nil) != valid {
					t.Errorf("test %d.%d: validity mismatch: have %v, want %v", i, j, result, valid)
				}
			case <-time.After(time.Second):
				t.Fatalf("test %d.%d: verification timeout", i, j)
			}

			select {
			case result := <-results:
				t.Fatalf("test %d.%d: unexpected result returned: %v", i, j, result)
			case <-time.After(25 * time.Millisecond):
			}
		}
		chain.InsertChain(blocks[i : i+1])
	}
}

func TestHeaderConcurrentVerification2(t *testing.T)  { testHeaderConcurrentVerification(t, 2) }
func TestHeaderConcurrentVerification8(t *testing.T)  { testHeaderConcurrentVerification(t, 8) }
func TestHeaderConcurrentVerification32(t *testing.T) { testHeaderConcurrentVerification(t, 32) }

func testHeaderConcurrentVerification(t *testing.T, threads int) {

	var (
		testdb, _ = aoadb.NewMemDatabase()
		gspec     = &Genesis{Config: params.TestChainConfig}
		genesis   = gspec.MustCommit(testdb)
		blocks, _ = GenerateChain(params.TestChainConfig, genesis, dpos.New(), testdb, 8, nil)
	)
	headers := make([]*types.Header, len(blocks))
	seals := make([]bool, len(blocks))

	for i, block := range blocks {
		headers[i] = block.Header()
		seals[i] = true
	}

	old := runtime.GOMAXPROCS(threads)
	defer runtime.GOMAXPROCS(old)

	for i, valid := range []bool{true, false} {
		var results <-chan error

		chain, _ := NewBlockChain(testdb, params.TestChainConfig, dpos.New(), vm.Config{},nil)
		_, results = chain.aoaEngine.VerifyHeaders(chain, headers)
		chain.Stop()

		checks := make(map[int]error)
		for j := 0; j < len(blocks); j++ {
			select {
			case result := <-results:
				checks[j] = result

			case <-time.After(time.Second):
				t.Fatalf("test %d.%d: verification timeout", i, j)
			}
		}

		for j := 0; j < len(blocks); j++ {
			want := valid || (j < len(blocks)-2)
			if (checks[j] == nil) != want {
				t.Errorf("test %d.%d: validity mismatch: have %v, want %v", i, j, checks[j], want)
			}
			if !want {

				break
			}
		}

		select {
		case result := <-results:
			t.Fatalf("test %d: unexpected result returned: %v", i, result)
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func TestHeaderConcurrentAbortion2(t *testing.T)  { testHeaderConcurrentAbortion(t, 2) }
func TestHeaderConcurrentAbortion8(t *testing.T)  { testHeaderConcurrentAbortion(t, 8) }
func TestHeaderConcurrentAbortion32(t *testing.T) { testHeaderConcurrentAbortion(t, 32) }

func testHeaderConcurrentAbortion(t *testing.T, threads int) {

	var (
		testdb, _ = aoadb.NewMemDatabase()
		gspec     = &Genesis{Config: params.TestChainConfig}
		genesis   = gspec.MustCommit(testdb)
		blocks, _ = GenerateChain(params.TestChainConfig, genesis, dpos.New(), testdb, 1024, nil)
	)
	headers := make([]*types.Header, len(blocks))
	seals := make([]bool, len(blocks))

	for i, block := range blocks {
		headers[i] = block.Header()
		seals[i] = true
	}

	old := runtime.GOMAXPROCS(threads)
	defer runtime.GOMAXPROCS(old)

	chain, _ := NewBlockChain(testdb, params.TestChainConfig, dpos.New(), vm.Config{},nil)
	defer chain.Stop()

	abort, results := chain.aoaEngine.VerifyHeaders(chain, headers)
	close(abort)

	verified := 0
	for depleted := false; !depleted; {
		select {
		case result := <-results:
			if result != nil {
				t.Errorf("header %d: validation failed: %v", verified, result)
			}
			verified++
		case <-time.After(50 * time.Millisecond):
			depleted = true
		}
	}

	if verified > 2*threads {
		t.Errorf("verification count too large: have %d, want below %d", verified, 2*threads)
	}
}
