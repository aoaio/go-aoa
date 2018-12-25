package params

import (
	"math/big"
	"reflect"
	"testing"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new *ChainConfig
		head        uint64
		wantErr     *ConfigCompatError
	}
	tests := []test{
		{stored: AllAuroraProtocolChanges, new: AllAuroraProtocolChanges, head: 0, wantErr: nil},
		{stored: AllAuroraProtocolChanges, new: AllAuroraProtocolChanges, head: 100, wantErr: nil},
		{
			stored:  &ChainConfig{},
			new:     &ChainConfig{AresBlock: big.NewInt(20)},
			head:    9,
			wantErr: nil,
		},
		{
			stored:  &ChainConfig{ChainId:big.NewInt(1), ByzantiumBlock: big.NewInt(4370000), AresBlock: big.NewInt(373300)},
			new:     &ChainConfig{ChainId:big.NewInt(1), ByzantiumBlock: big.NewInt(4370000), AresBlock: big.NewInt(373300), EpiphronBlock: big.NewInt(400000)},
			head:    390000,
			wantErr: nil,
		},
		{
			stored:  &ChainConfig{ChainId:big.NewInt(1), ByzantiumBlock: big.NewInt(4370000), AresBlock: big.NewInt(373300), BlockInterval:big.NewInt(10)},
			new:     &ChainConfig{ChainId:big.NewInt(1), ByzantiumBlock: big.NewInt(4370000), AresBlock: big.NewInt(373300), BlockInterval:big.NewInt(5)},
			head:    390000,
			wantErr: nil,
		},
		{
			stored: AllAuroraProtocolChanges,
			new:    &ChainConfig{},
			head:   3,
			wantErr: &ConfigCompatError{
				What:         "Epiphron fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored: AllAuroraProtocolChanges,
			new:    &ChainConfig{ByzantiumBlock: big.NewInt(10000),AresBlock:big.NewInt(90), EpiphronBlock:big.NewInt(1)},
			head:   3,
			wantErr: &ConfigCompatError{
				What:         "Epiphron fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    big.NewInt(1),
				RewindTo:     0,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.head)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nhead: %v\nerr: %v\nwant: %v", test.stored, test.new, test.head, err, test.wantErr)
		}
	}
}
