package types

import (
	"github.com/Aurorachain/go-Aurora/common"
	"math/big"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
)

type InnerTx struct {
	From		common.Address	`json:"from" gencodec:"required"`
	To 			common.Address	`json:"to" gencodec:"to" gencodec:"required"`
	AssetID		*common.Address	`json:"assetid" rlp:"nil"`
	Value		*big.Int		`json:"value" gencodec:"required"`
}

type innertxMarshaling struct {
	Value		*hexutil.Big
}