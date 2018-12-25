package simulations

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Aurorachain/go-Aurora/p2p/discover"
)

func TestMocker(t *testing.T) {

	_, s := testHTTPServer(t)
	defer s.Close()

	client := NewClient(s.URL)

	err := client.StartNetwork()
	if err != nil {
		t.Fatalf("Could not start test network: %s", err)
	}

	defer func() {
		err = client.StopNetwork()
		if err != nil {
			t.Fatalf("Could not stop test network: %s", err)
		}
	}()

	resp, err := http.Get(s.URL + "/mocker")
	if err != nil {
		t.Fatalf("Could not get mocker list: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Invalid Status Code received, expected 200, got %d", resp.StatusCode)
	}

	var mockerlist []string
	err = json.NewDecoder(resp.Body).Decode(&mockerlist)
	if err != nil {
		t.Fatalf("Error decoding JSON mockerlist: %s", err)
	}

	if len(mockerlist) < 1 {
		t.Fatalf("No mockers available")
	}

	nodeCount := 10
	var wg sync.WaitGroup

	events := make(chan *Event, 10)
	var opts SubscribeOpts
	sub, err := client.SubscribeNetwork(events, opts)
	defer sub.Unsubscribe()

	nodemap := make(map[discover.NodeID]bool)
	wg.Add(1)
	nodesComplete := false
	connCount := 0
	go func() {
		for {
			select {
			case event := <-events:

				if event.Node != nil && event.Node.Up {

					nodemap[event.Node.Config.ID] = true

					if len(nodemap) == nodeCount {
						nodesComplete = true

					}
				} else if event.Conn != nil && nodesComplete {
					connCount += 1
					if connCount == (nodeCount-1)*2 {
						wg.Done()
						return
					}
				}
			case <-time.After(30 * time.Second):
				wg.Done()
				t.Fatalf("Timeout waiting for nodes being started up!")
			}
		}
	}()

	mockertype := mockerlist[len(mockerlist)-1]

	for _, m := range mockerlist {
		if m == "probabilistic" {
			mockertype = m
			break
		}
	}

	resp, err = http.PostForm(s.URL+"/mocker/start", url.Values{"mocker-type": {mockertype}, "node-count": {strconv.Itoa(nodeCount)}})
	if err != nil {
		t.Fatalf("Could not start mocker: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Invalid Status Code received for starting mocker, expected 200, got %d", resp.StatusCode)
	}

	wg.Wait()

	nodes_info, err := client.GetNodes()
	if err != nil {
		t.Fatalf("Could not get nodes list: %s", err)
	}

	if len(nodes_info) != nodeCount {
		t.Fatalf("Expected %d number of nodes, got: %d", nodeCount, len(nodes_info))
	}

	resp, err = http.Post(s.URL+"/mocker/stop", "", nil)
	if err != nil {
		t.Fatalf("Could not stop mocker: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Invalid Status Code received for stopping mocker, expected 200, got %d", resp.StatusCode)
	}

	_, err = http.Post(s.URL+"/reset", "", nil)
	if err != nil {
		t.Fatalf("Could not reset network: %s", err)
	}

	nodes_info, err = client.GetNodes()
	if err != nil {
		t.Fatalf("Could not get nodes list: %s", err)
	}

	if len(nodes_info) != 0 {
		t.Fatalf("Expected empty list of nodes, got: %d", len(nodes_info))
	}
}
