package simulations

import (
	"context"
	"time"

	"github.com/Aurorachain/go-Aurora/p2p/discover"
)

type Simulation struct {
	network *Network
}

func NewSimulation(network *Network) *Simulation {
	return &Simulation{
		network: network,
	}
}

func (s *Simulation) Run(ctx context.Context, step *Step) (result *StepResult) {
	result = newStepResult()

	result.StartedAt = time.Now()
	defer func() { result.FinishedAt = time.Now() }()

	stop := s.watchNetwork(result)
	defer stop()

	if err := step.Action(ctx); err != nil {
		result.Error = err
		return
	}

	nodes := make(map[discover.NodeID]struct{}, len(step.Expect.Nodes))
	for _, id := range step.Expect.Nodes {
		nodes[id] = struct{}{}
	}
	for len(result.Passes) < len(nodes) {
		select {
		case id := <-step.Trigger:

			if _, ok := nodes[id]; !ok {
				continue
			}

			if _, ok := result.Passes[id]; ok {
				continue
			}

			pass, err := step.Expect.Check(ctx, id)
			if err != nil {
				result.Error = err
				return
			}
			if pass {
				result.Passes[id] = time.Now()
			}
		case <-ctx.Done():
			result.Error = ctx.Err()
			return
		}
	}

	return
}

func (s *Simulation) watchNetwork(result *StepResult) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	events := make(chan *Event)
	sub := s.network.Events().Subscribe(events)
	go func() {
		defer close(done)
		defer sub.Unsubscribe()
		for {
			select {
			case event := <-events:
				result.NetworkEvents = append(result.NetworkEvents, event)
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

type Step struct {

	Action func(context.Context) error

	Trigger chan discover.NodeID

	Expect *Expectation
}

type Expectation struct {

	Nodes []discover.NodeID

	Check func(context.Context, discover.NodeID) (bool, error)
}

func newStepResult() *StepResult {
	return &StepResult{
		Passes: make(map[discover.NodeID]time.Time),
	}
}

type StepResult struct {

	Error error

	StartedAt time.Time

	FinishedAt time.Time

	Passes map[discover.NodeID]time.Time

	NetworkEvents []*Event
}
