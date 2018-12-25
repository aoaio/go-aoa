package vm

import "testing"

func TestMemoryGasCost(t *testing.T) {

	size := uint64(0xffffffffe0)
	v, err := memoryGasCost(&Memory{}, size)
	if err != nil {
		t.Error("didn't expect error:", err)
	}
	if v != 18014432802111487 {
		t.Errorf("Expected: 18014432802111487, got %d", v)
	}

	_, err = memoryGasCost(&Memory{}, size+1)
	if err == nil {
		t.Error("expected error")
	}
}
