package vm

import (
	"math/big"

	"github.com/Aurorachain/go-Aurora/params"
)

const (
	GasQuickStep   uint64 = 1
	GasFastestStep uint64 = 1
	GasFastStep    uint64 = 2
	GasMidStep     uint64 = 3
	GasSlowStep    uint64 = 4
	GasExtStep     uint64 = 7

	GasReturn       uint64 = 0
	GasStop         uint64 = 0
	GasContractByte uint64 = 32
)

func callGas(gasTable params.GasTable, availableGas, base uint64, callCost *big.Int) (uint64, error) {
	if gasTable.CreateBySuicide > 0 {
		availableGas = availableGas - base
		gas := availableGas - availableGas/64

		if callCost.BitLen() > 64 || gas < callCost.Uint64() {
			return gas, nil
		}
	}
	if callCost.BitLen() > 64 {
		return 0, errGasUintOverflow
	}

	return callCost.Uint64(), nil
}
