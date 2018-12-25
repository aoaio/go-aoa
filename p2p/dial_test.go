package p2p

import (
	"encoding/binary"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/Aurorachain/go-Aurora/p2p/discover"
	"github.com/Aurorachain/go-Aurora/p2p/netutil"
	"github.com/davecgh/go-spew/spew"
)

func init() {
	spew.Config.Indent = "\t"
}

type dialtest struct {
	init   *dialstate
	rounds []round
}

type round struct {
	peers []*Peer
	done  []task
	new   []task
}

func runDialTest(t *testing.T, test dialtest) {
	var (
		vtime   time.Time
		running int
	)
	pm := func(ps []*Peer) map[discover.NodeID]*Peer {
		m := make(map[discover.NodeID]*Peer)
		for _, p := range ps {
			m[p.rw.id] = p
		}
		return m
	}
	for i, round := range test.rounds {
		for _, task := range round.done {
			running--
			if running < 0 {
				panic("running task counter underflow")
			}
			test.init.taskDone(task, vtime)
		}

		new := test.init.newTasks(running, pm(round.peers), vtime,discover.ConsNet)
		if !sametasks(new, round.new) {
			t.Errorf("round %d: new tasks mismatch:\ngot %v\nwant %v\nstate: %v\nrunning: %v\n",
				i, spew.Sdump(new), spew.Sdump(round.new), spew.Sdump(test.init), spew.Sdump(running))
		}

		vtime = vtime.Add(16 * time.Second)
		running += len(new)
	}
}

type fakeTable []*discover.Node

func (t fakeTable) Self() *discover.Node                     { return new(discover.Node) }
func (t fakeTable) Close()                                   {}
func (t fakeTable) Lookup(discover.NodeID , byte) []*discover.Node  { return nil }
func (t fakeTable) Resolve(discover.NodeID) *discover.Node   { return nil }
func (t fakeTable) ReadRandomNodes(buf []*discover.Node,tyoe byte) int { return copy(buf, t) }

func TestDialStateDynDial(t *testing.T) {
	runDialTest(t, dialtest{
		init: newDialState(nil, nil, fakeTable{}, 5, nil),
		rounds: []round{

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet,}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet,}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet,}},
				},
				new: []task{&discoverTask{netType:discover.ConsNet}},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet,}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet,}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet,}},
				},
				done: []task{
					&discoverTask{results: []*discover.Node{
						{ID: uintID(2)},
						{ID: uintID(3)},
						{ID: uintID(4)},
						{ID: uintID(5)},
						{ID: uintID(6)},
						{ID: uintID(7)},
					},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(3),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(4),netType:discover.ConsNet}},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(3)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(4)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(5)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
				new: []task{
					&waitExpireTask{Duration: 14 * time.Second,netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(3),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(4),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(5),netType:discover.ConsNet}},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(6)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(5),netType:discover.ConsNet}},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(6)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(7)},netType:discover.ConsNet},
					&discoverTask{netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(5)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(7)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(7)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(5)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(7)},netType:discover.ConsNet},
				},
				done: []task{
					&discoverTask{netType:discover.ConsNet},
				},
				new: []task{
					&discoverTask{netType:discover.ConsNet},
				},
			},
		},
	})
}

func TestDialStateDynDialBootnode(t *testing.T) {
	bootnodes := []*discover.Node{
		{ID: uintID(1)},
		{ID: uintID(2)},
		{ID: uintID(3)},
	}
	table := fakeTable{
		{ID: uintID(4)},
		{ID: uintID(5)},
		{ID: uintID(6)},
		{ID: uintID(7)},
		{ID: uintID(8)},
	}
	runDialTest(t, dialtest{
		init: newDialState(nil, bootnodes, table, 5, nil),
		rounds: []round{

			{
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
					&discoverTask{netType:discover.ConsNet},
				},
			},

			{
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},

			{},

			{
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},

			{
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
				},
			},

			{
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
			},

			{
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(4)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},
		},
	})
}

func TestDialStateDynDialFromTable(t *testing.T) {

	table := fakeTable{
		{ID: uintID(1)},
		{ID: uintID(2)},
		{ID: uintID(3)},
		{ID: uintID(4)},
		{ID: uintID(5)},
		{ID: uintID(6)},
		{ID: uintID(7)},
		{ID: uintID(8)},
	}

	runDialTest(t, dialtest{
		init: newDialState(nil, nil, table, 10, nil),
		rounds: []round{

			{
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
					&discoverTask{netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet}},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
					&discoverTask{results: []*discover.Node{
						{ID: uintID(10)},
						{ID: uintID(11)},
						{ID: uintID(12)},
					}},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(10)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(11)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(12)},netType:discover.ConsNet},
					&discoverTask{netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(10)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(11)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(12)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(10)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(11)},netType:discover.ConsNet},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(12)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(10),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(11),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(12),netType:discover.ConsNet}},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(10),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(11),netType:discover.ConsNet}},
					{rw: &conn{flags: dynDialedConn, id: uintID(12),netType:discover.ConsNet}},
				},
			},
		},
	})
}

func TestDialStateNetRestrict(t *testing.T) {

	table := fakeTable{
		{ID: uintID(1), IP: net.ParseIP("127.0.0.1")},
		{ID: uintID(2), IP: net.ParseIP("127.0.0.2")},
		{ID: uintID(3), IP: net.ParseIP("127.0.0.3")},
		{ID: uintID(4), IP: net.ParseIP("127.0.0.4")},
		{ID: uintID(5), IP: net.ParseIP("127.0.2.5")},
		{ID: uintID(6), IP: net.ParseIP("127.0.2.6")},
		{ID: uintID(7), IP: net.ParseIP("127.0.2.7")},
		{ID: uintID(8), IP: net.ParseIP("127.0.2.8")},
	}
	restrict := new(netutil.Netlist)
	restrict.Add("127.0.2.0/24")

	runDialTest(t, dialtest{
		init: newDialState(nil, nil, table, 10, restrict),
		rounds: []round{
			{
				new: []task{
					&dialTask{flags: dynDialedConn, dest: table[4],netType:discover.ConsNet},
					&discoverTask{netType:discover.ConsNet},
				},
			},
		},
	})
}

func TestDialStateStaticDial(t *testing.T) {
	wantStatic := []*discover.Node{
		{ID: uintID(1)},
		{ID: uintID(2)},
		{ID: uintID(3)},
		{ID: uintID(4)},
		{ID: uintID(5)},
	}

	runDialTest(t, dialtest{
		init: newDialState(wantStatic, nil, fakeTable{}, 0, nil),
		rounds: []round{

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)}},
				},
				new: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(3)},netType:discover.ConsNet},

				},
				done: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(3)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(4)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(5)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(5)},netType:discover.ConsNet},
				},
				new: []task{
					&waitExpireTask{Duration: 14 * time.Second,netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(3)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(4)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(5)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(3)},netType:discover.ConsNet},
					{rw: &conn{flags: staticDialedConn, id: uintID(5)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(4)},netType:discover.ConsNet},
				},
			},
		},
	})
}

func TestDialStateCache(t *testing.T) {
	wantStatic := []*discover.Node{
		{ID: uintID(1)},
		{ID: uintID(2)},
		{ID: uintID(3)},
	}

	runDialTest(t, dialtest{
		init: newDialState(wantStatic, nil, fakeTable{}, 0, nil),
		rounds: []round{

			{
				peers: nil,
				new: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(1),netType:discover.ConsNet}},
					{rw: &conn{flags: staticDialedConn, id: uintID(2),netType:discover.ConsNet}},
				},
				done: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(1)},netType:discover.ConsNet},
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(2)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
				},
				done: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
				new: []task{
					&waitExpireTask{Duration: 14 * time.Second,netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
				},
			},

			{
				peers: []*Peer{
					{rw: &conn{flags: dynDialedConn, id: uintID(1)},netType:discover.ConsNet},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)},netType:discover.ConsNet},
				},
				new: []task{
					&dialTask{flags: staticDialedConn, dest: &discover.Node{ID: uintID(3)},netType:discover.ConsNet},
				},
			},
		},
	})
}

func TestDialResolve(t *testing.T) {
	resolved := discover.NewNode(uintID(1), net.IP{127, 0, 55, 234}, 3333, 4444)
	table := &resolveMock{answer: resolved}
	state := newDialState(nil, nil, table, 0, nil)

	dest := discover.NewNode(uintID(1), nil, 0, 0)
	state.addStatic(dest)
	tasks := state.newTasks(0, nil, time.Time{},discover.ConsNet)
	if !reflect.DeepEqual(tasks, []task{&dialTask{flags: staticDialedConn, dest: dest,netType:discover.ConsNet}}) {
		t.Fatalf("expected dial task, got %#v", tasks)
	}

	config := Config{Dialer: TCPDialer{&net.Dialer{Deadline: time.Now().Add(-5 * time.Minute)}}}
	srv := &Server{ntab: table, Config: config}
	tasks[0].Do(srv)
	if !reflect.DeepEqual(table.resolveCalls, []discover.NodeID{dest.ID}) {
		t.Fatalf("wrong resolve calls, got %v", table.resolveCalls)
	}

	state.taskDone(tasks[0], time.Now())
	if state.static[uintID(1)].dest != resolved {
		t.Fatalf("state.dest not updated")
	}
}

func sametasks(a, b []task) bool {
	if len(a) != len(b) {
		return false
	}
next:
	for _, ta := range a {
		for _, tb := range b {
			if reflect.DeepEqual(ta, tb) {
				continue next
			}
		}
		return false
	}
	return true
}

func uintID(i uint32) discover.NodeID {
	var id discover.NodeID
	binary.BigEndian.PutUint32(id[:], i)
	return id
}

type resolveMock struct {
	resolveCalls []discover.NodeID
	answer       *discover.Node
}

func (t *resolveMock) Resolve(id discover.NodeID) *discover.Node {
	t.resolveCalls = append(t.resolveCalls, id)
	return t.answer
}

func (t *resolveMock) Self() *discover.Node                     { return new(discover.Node) }
func (t *resolveMock) Close()                                   {}
func (t *resolveMock) Bootstrap([]*discover.Node)               {}
func (t *resolveMock) Lookup(discover.NodeID,byte) []*discover.Node  { return nil }
func (t *resolveMock) ReadRandomNodes(buf []*discover.Node,netType byte) int { return 0 }
