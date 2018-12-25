package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/common/math"
	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/crypto/sha3"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/util"
	"math/big"
	"strings"
)

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

const genesisExtra = "AOA genesis"

type Genesis struct {
	Config    *params.ChainConfig `json:"config"`
	Timestamp uint64              `json:"timestamp"`
	ExtraData []byte              `json:"extraData"`
	GasLimit  uint64              `json:"gasLimit"   gencodec:"required"`
	Coinbase  common.Address      `json:"coinbase"`
	Alloc     GenesisAlloc        `json:"alloc"      gencodec:"required"`
	Agents    GenesisAgents       `json:"agents"     gencodec:"required"`

	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
}

type GenesisAlloc map[common.Address]GenesisAccount

type GenesisAgents []types.Candidate

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"`
}

type genesisSpecMarshaling struct {
	Timestamp math.HexOrDecimal64
	ExtraData hexutil.Bytes
	GasLimit  math.HexOrDecimal64
	GasUsed   math.HexOrDecimal64
	Number    math.HexOrDecimal64
	Alloc     map[common.UnprefixedAddress]GenesisAccount
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2
	if _, err := hex.Decode(h[offset:], text); err != nil {
		fmt.Println(err)
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database already contains an incompatible genesis block (have %x, new %x)", e.Stored[:8], e.New[:8])
}

func SetupGenesisBlock(db aoadb.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, *Genesis, error) {
	if genesis != nil && genesis.Config == nil {
		return params.AllAuroraProtocolChanges, common.Hash{}, genesis, errGenesisNoConfig
	}

	stored := GetCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		if genesis == nil {
			log.Info("Writing default main-net genesis block")
			genesis = DefaultGenesisBlock()
		} else {
			log.Info("Writing custom genesis block")
		}
		block, err := genesis.Commit(db)
		return genesis.Config, block.Hash(), genesis, err
	}

	if genesis != nil {
		block, _, _ := genesis.ToBlock()
		hash := block.Hash()
		if hash != stored {
			return genesis.Config, block.Hash(), genesis, &GenesisMismatchError{stored, hash}
		}
	}

	newcfg := genesis.configOrDefault(stored)
	storedcfg, err := GetChainConfig(db, stored)
	if err != nil {
		if err == ErrChainConfigNotFound {

			log.Warn("Found genesis block without chain config")
			err = WriteChainConfig(db, stored, newcfg)
		}
		return newcfg, stored, genesis, err
	}

	if genesis == nil && stored != params.MainnetGenesisHash {
		return storedcfg, stored, genesis, nil
	}

	height := GetBlockNumber(db, GetHeadHeaderHash(db))
	if height == missingNumber {
		return newcfg, stored, genesis, fmt.Errorf("missing block number for head header hash")
	}
	compatErr := storedcfg.CheckCompatible(newcfg, height)
	if compatErr != nil && height != 0 && compatErr.RewindTo != 0 {
		return newcfg, stored, genesis, compatErr
	}
	return newcfg, stored, genesis, WriteChainConfig(db, stored, newcfg)
}

func (g *Genesis) configOrDefault(ghash common.Hash) *params.ChainConfig {
	switch {
	case g != nil:
		return g.Config
	case ghash == params.MainnetGenesisHash:
		return params.MainnetChainConfig
	case ghash == params.TestnetGenesisHash:
		return params.TestnetChainConfig
	default:
		return params.AllAuroraProtocolChanges
	}
}

func (g *Genesis) ToBlock() (*types.Block, *state.StateDB, *delegatestate.DelegateDB) {
	db, _ := aoadb.NewMemDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db))
	delegatedb, _ := delegatestate.New(common.Hash{}, delegatestate.NewDatabase(db))
	for addr, account := range g.Alloc {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root := statedb.IntermediateRoot(false)

	for _, agent := range g.Agents {
		addr := common.HexToAddress(agent.Address)
		obj := delegatedb.GetOrNewStateObject(addr, agent.Nickname, agent.RegisterTime)
		obj.AddVote(big.NewInt(int64(agent.Vote)))
	}
	delegateRoot := delegatedb.IntermediateRoot(false)
	topDelegates := delegatedb.GetDelegates()
	config := g.Config
	MaxElectDelegate := config.MaxElectDelegate.Int64()
	if len(topDelegates) > int(MaxElectDelegate) {
		topDelegates = topDelegates[:int(MaxElectDelegate)]
	}
	shuffleNewRound := util.ShuffleNewRound(int64(g.Timestamp), int(MaxElectDelegate), topDelegates, config.BlockInterval.Int64())
	shuffleList := types.ShuffleList{ShuffleDels: shuffleNewRound}
	rlpShufflehash := rlpHash(shuffleList)

	encodeBytes := hexutil.Encode([]byte("AOA official"))
	agentName := hexutil.MustDecode(encodeBytes)

	head := &types.Header{
		Number:             new(big.Int).SetUint64(g.Number),
		Time:               new(big.Int).SetUint64(g.Timestamp),
		ParentHash:         g.ParentHash,
		Extra:              g.ExtraData,
		GasLimit:           g.GasLimit,
		GasUsed:            g.GasUsed,
		Coinbase:           g.Coinbase,
		Root:               root,
		DelegateRoot:       delegateRoot,
		ShuffleHash:        rlpShufflehash,
		AgentName:          agentName,
		ShuffleBlockNumber: big.NewInt(int64(g.Number)),
	}
	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	return types.NewBlock(head, nil, nil), statedb, delegatedb
}

func (g *Genesis) Commit(db aoadb.Database) (*types.Block, error) {
	block, statedb, delegatedb := g.ToBlock()
	if block.Number().Sign() != 0 {
		return nil, fmt.Errorf("can't commit genesis block with number > 0")
	}
	if _, err := statedb.CommitTo(db, false); err != nil {
		return nil, fmt.Errorf("cannot write state: %v", err)
	}
	if _, err := delegatedb.CommitTo(db, false); err != nil {
		return nil, fmt.Errorf("cannot write delegate state: %v", err)
	}
	if err := WriteTd(db, block.Hash(), block.NumberU64(), types.BlockDifficult); err != nil {
		return nil, err
	}
	if err := WriteBlock(db, block); err != nil {
		return nil, err
	}
	if err := WriteBlockReceipts(db, block.Hash(), block.NumberU64(), nil); err != nil {
		return nil, err
	}
	if err := WriteCanonicalHash(db, block.Hash(), block.NumberU64()); err != nil {
		return nil, err
	}
	if err := WriteHeadBlockHash(db, block.Hash()); err != nil {
		return nil, err
	}
	if err := WriteHeadHeaderHash(db, block.Hash()); err != nil {
		return nil, err
	}
	config := g.Config
	if config == nil {
		config = params.AllAuroraProtocolChanges
	}
	return block, WriteChainConfig(db, block.Hash(), config)
}

func (g *Genesis) MustCommit(db aoadb.Database) *types.Block {
	block, err := g.Commit(db)
	if err != nil {
		panic(err)
	}
	return block
}

func GenesisBlockForTesting(db aoadb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{Alloc: GenesisAlloc{addr: {Balance: balance}}}
	return g.MustCommit(db)
}

func DefaultGenesisBlock() *Genesis {
	encodeBytes := hexutil.Encode([]byte(genesisExtra))
	agentName := hexutil.MustDecode(encodeBytes)
	return &Genesis{
		Config:    params.MainnetChainConfig,
		ExtraData: agentName,
		GasLimit:  params.GenesisGasLimit,
		Alloc:     decodePrealloc(mainnetAllocData),
		Agents:    decodeGenesisAgents(mainnetAgentData),
		Timestamp: 0,
	}
}

func DefaultTestnetGenesisBlock() *Genesis {
	encodeBytes := hexutil.Encode([]byte(genesisExtra))
	agentName := hexutil.MustDecode(encodeBytes)
	return &Genesis{
		Config:    params.TestnetChainConfig,
		ExtraData: agentName,
		GasLimit:  250000000,
		Alloc:     decodePrealloc(testnetAllocData),
		Agents:    decodeGenesisAgents(testnetAgentData),
		Timestamp: 0,
	}
}

func DefaultRinkebyGenesisBlock() *Genesis {
	encodeBytes := hexutil.Encode([]byte(genesisExtra))
	agentName := hexutil.MustDecode(encodeBytes)
	return &Genesis{
		Config:    params.AllAuroraProtocolChanges,
		Timestamp: 1492009146,
		ExtraData: agentName,
		GasLimit:  4700000,
		Alloc:     decodePrealloc(rinkebyAllocData),
	}
}

func DeveloperGenesisBlock(developer common.Address) *Genesis {

	config := *params.AllAuroraProtocolChanges
	encodeBytes := hexutil.Encode([]byte(genesisExtra))
	agentName := hexutil.MustDecode(encodeBytes)
	var geneAgents GenesisAgents
	candidates := make([]types.Candidate, 0)
	candidates = append(candidates, types.Candidate{Address: strings.ToLower(developer.Hex()), Vote: 1, Nickname: developer.Hex(), RegisterTime: uint64(11111112111)})
	geneAgents = append(geneAgents, candidates...)

	return &Genesis{
		Config:    &config,
		ExtraData: agentName,
		GasLimit:  250000000,
		Alloc: map[common.Address]GenesisAccount{
			developer: {Balance: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(9))},
		},
		Agents:    geneAgents,
		Timestamp: 1492009146,
	}
}

func decodePrealloc(data string) GenesisAlloc {
	var p []struct{ Addr, Balance *big.Int }
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	ga := make(GenesisAlloc, len(p))
	for _, account := range p {
		ga[common.BigToAddress(account.Addr)] = GenesisAccount{Balance: account.Balance}
	}
	return ga
}

func decodeGenesisAgents(data string) GenesisAgents {
	var p GenesisAgents
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	return p
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}
