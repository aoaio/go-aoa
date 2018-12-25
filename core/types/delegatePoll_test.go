package types

import (
	"fmt"
	"sort"
	"testing"
)

func TestCandidateSlice_Less(t *testing.T) {
	candidateList := []Candidate{
		{"0x34f6feaa439ea2e92438365933067acaff5e3b7c", uint64(1), "node1", 1492009146},
		{"0xb34822fea9f8aaae7c7f64a097f64e5dffb6f344", uint64(2), "node2", 1492009146},
		{"0x0ac71830f52bda2046583d7cb2df07855922f74a", uint64(1), "node3", 1492009146},
		{"0xa6a6d6134f0c09500af2304e3f62398e24f8def1", uint64(1), "node1", 1492009146},
		{"0xdefee9edbf3a6da3a5bb96d006b86ac884d14f64", uint64(1), "node2", 1492009146},
	}

	sort.Sort(CandidateSlice(candidateList))
	for _, v := range candidateList {
		fmt.Println(v)
	}
}
