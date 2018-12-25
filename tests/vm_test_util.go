package tests

import (
	"encoding/json"
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/common/math"
	"github.com/Aurorachain/go-Aurora/core"
	"github.com/Aurorachain/go-Aurora/crypto"
)

type VMTest struct {
	json vmJSON
}

func (t *VMTest) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &t.json)
}

type vmJSON struct {
	Env           stEnv                 `json:"env"`
	Exec          vmExec                `json:"exec"`
	Logs          common.UnprefixedHash `json:"logs"`
	GasRemaining  *math.HexOrDecimal64  `json:"gas"`
	Out           hexutil.Bytes         `json:"out"`
	Pre           core.GenesisAlloc     `json:"pre"`
	Post          core.GenesisAlloc     `json:"post"`
	PostStateRoot common.Hash           `json:"postStateRoot"`
}

//go:generate gencodec -type vmExec -field-override vmExecMarshaling -out gen_vmexec.go

type vmExec struct {
	Address  common.Address `json:"address"  gencodec:"required"`
	Caller   common.Address `json:"caller"   gencodec:"required"`
	Origin   common.Address `json:"origin"   gencodec:"required"`
	Code     []byte         `json:"code"     gencodec:"required"`
	Data     []byte         `json:"data"     gencodec:"required"`
	Value    *big.Int       `json:"value"    gencodec:"required"`
	GasLimit uint64         `json:"gas"      gencodec:"required"`
	GasPrice *big.Int       `json:"gasPrice" gencodec:"required"`
}

type vmExecMarshaling struct {
	Address  common.UnprefixedAddress
	Caller   common.UnprefixedAddress
	Origin   common.UnprefixedAddress
	Code     hexutil.Bytes
	Data     hexutil.Bytes
	Value    *math.HexOrDecimal256
	GasLimit math.HexOrDecimal64
	GasPrice *math.HexOrDecimal256
}

func vmTestBlockHash(n uint64) common.Hash {
	return common.BytesToHash(crypto.Keccak256([]byte(big.NewInt(int64(n)).String())))
}