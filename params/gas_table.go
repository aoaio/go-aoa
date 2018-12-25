package params

type GasTable struct {
	ExtcodeSize uint64
	ExtcodeCopy uint64
	Balance     uint64
	SLoad       uint64
	Calls       uint64
	Suicide     uint64

	ExpByte uint64

	CreateBySuicide uint64
}

var GasTableEpiphron = GasTable{
	ExtcodeSize: 45,
	ExtcodeCopy: 45,
	Balance:     25,
	SLoad:       20,
	Calls:       45,
	Suicide:     350,
	ExpByte:     4,

	CreateBySuicide: 2500,
}
