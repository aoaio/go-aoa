package watch

import (
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/rlp"
	"errors"
)

type InnerTxDb interface {
	Set(txhash common.Hash,itxs []*types.InnerTx) error
	Has(txhash common.Hash) (bool,error)
	Get(txhash common.Hash) ([]*types.InnerTx,error)
}

type itxdb struct {
	db aoadb.Database
}

func NewInnerTxDb(db aoadb.Database) InnerTxDb {
	return &itxdb{db:db}
}

func (db *itxdb)Set(txhash common.Hash,itxs []*types.InnerTx) error {
	if len(itxs) > 0 {
		v,err := rlp.EncodeToBytes(itxs)
		if nil != err {
			return err
		}
		err = db.db.Put(txhash.Bytes(),v)
		return err
	} else {
		return errors.New("no value to save")
	}
}

func (db *itxdb) Has(txhash common.Hash) (bool, error) {
	k := txhash.Bytes()
	return db.db.Has(k)
}

func (db *itxdb) Get(txhash common.Hash) ([]*types.InnerTx,error) {
	k := txhash.Bytes()
	v,err := db.db.Get(k)
	if nil != err {
		return nil,err
	}
	var itxs []*types.InnerTx
	err = rlp.DecodeBytes(v,&itxs)
	return itxs,err
}
