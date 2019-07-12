package aoadb

import (
	"github.com/Aurorachain/go-aoa/aoadb/dbinterface"
)

const IdealBatchSize = 100 * 1024

type Database interface {
	dbinterface.Database
}

type MemDatabase interface {
	dbinterface.Database
	Len() int
}
