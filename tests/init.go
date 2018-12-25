package tests

import (
	"fmt"
	"math/big"

	"github.com/Aurorachain/go-Aurora/params"
)

var Forks = map[string]*params.ChainConfig{
	"Frontier": {
		ChainId: big.NewInt(1),
	},
}

type UnsupportedForkError struct {
	Name string
}

func (e UnsupportedForkError) Error() string {
	return fmt.Sprintf("unsupported fork %q", e.Name)
}
