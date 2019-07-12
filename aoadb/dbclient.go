package aoadb

import (
	"github.com/Aurorachain/go-aoa/aoadb/dbinterface"
	"github.com/Aurorachain/go-aoa/aoadb/leveldb"
	"github.com/Aurorachain/go-aoa/aoadb/memorydb"
)

type NewCommonDB struct {
	dbinterface.KeyValueStore
}

func NewLevelDBDatabase(file string, cache int, handles int) (Database, error) {
	db, err := leveldb.New(file, cache, handles)
	if err != nil {
		return nil, err
	}
	return NewDatabase(db), nil
}

func NewDatabase(db dbinterface.KeyValueStore) Database {
	return &NewCommonDB{
		KeyValueStore: db,
	}
}

func NewMemDatabase() (MemDatabase, error) {
	return memorydb.New(), nil
}

func NewMemDatabaseWithCap(size int) (MemDatabase, error) {
	return memorydb.NewWithCap(size), nil
}
