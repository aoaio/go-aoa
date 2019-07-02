// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync/atomic"

	"encoding/json"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
	"github.com/Aurorachain/go-aoa/rlp"
	"sort"
)

//go:generate gencodec -type txdata -field-override txdataMarshaling -out gen_tx_json.go

const (
	ActionTrans = iota
	ActionRegister
	ActionAddVote
	ActionSubVote
	ActionPublishAsset
	ActionCreateContract
	ActionCallContract
)

const (
	RegisterAgent  = "Register Agent"
	VoteAgent      = "Vote Agent"
	CreateContract = "Create Contract"
	PublishAsset   = "Publish Asset"
)

var (
	ErrInvalidSig = errors.New("invalid transaction v, r, s values")
	errNoSigner   = errors.New("missing signing methods")
)

// deriveSigner makes a *best* guess about which signer to use.
func deriveSigner(V *big.Int) Signer {
	if V.Sign() != 0 {
		return NewAuroraSigner(deriveChainId(V))
	}
	return NewAuroraSigner(big.NewInt(1))
}

type Transaction struct {
	data txdata
	// caches
	hash       atomic.Value
	size       atomic.Value
	from       atomic.Value
	isContract atomic.Value
}

type Vote struct {
	Candidate *common.Address `json:"candidate"`
	Operation uint            `json:"operation"` //0,投票;1,取消投票
}

func VoteToBytes(vote []Vote) ([]byte, error) {
	enc, err := json.Marshal(vote)
	if err != nil {
		return nil, err
	}
	return enc, nil
}

func BytesToVote(enc []byte) ([]Vote, error) {
	vote := make([]Vote, 0)
	votePoint := &vote
	err := json.Unmarshal(enc, votePoint)
	if err != nil {
		return nil, err
	}
	return vote, nil
}

func AssetInfoToBytes(assetInfo AssetInfo) ([]byte, error) {
	enc, err := json.Marshal(assetInfo)
	if err != nil {
		return nil, err
	}
	return enc, nil
}

func BytesToAssetInfo(enc []byte) (*AssetInfo, error) {
	var assetInfo AssetInfo
	err := json.Unmarshal(enc, &assetInfo)
	if err != nil {
		return nil, err
	}
	return &assetInfo, nil
}

type txdata struct {
	AccountNonce uint64          `json:"nonce"    gencodec:"required"`
	Price        *big.Int        `json:"gasPrice" gencodec:"required"`
	GasLimit     uint64          `json:"gas"      gencodec:"required"`
	Recipient    *common.Address `json:"to"       rlp:"nil"` // nil means contract creation
	Amount       *big.Int        `json:"value"    gencodec:"required"`
	Payload      []byte          `json:"input"    gencodec:"required"`

	Action   uint64 `json:"action"  gencodec:"required"` // 参见当前包（当前文件）ActionXXX 常量定义
	Vote     []byte `json:"vote" rlp:"nil"`
	Nickname []byte `json:"nickname" rlp:"nil"`

	//资产符号，作为资产的唯一标识。当Action 为ActionTrans时有意义。
	Asset *common.Address `json:"asset,omitempty" rlp:"nil"`
	//资产信息，当Action 为 ActionPublishAsset 时有意义
	AssetInfo []byte `json:"assetInfo,omitempty" rlp:"nil"`
	//子地址，做归集资金使用
	SubAddress string `json:"subAddress,omitempty" rlp:"nil"`
	// When create a contract, user can offer the ABI so that it can store on the block
	Abi string `json:"abi,omitempty" rlp:"nil"`

	// Signature values
	V *big.Int `json:"v" gencodec:"required"` // chainId
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

	// This is only used when marshaling to JSON.
	Hash *common.Hash `json:"hash" rlp:"-"`
}

type txdataMarshaling struct {
	AccountNonce hexutil.Uint64
	Price        *hexutil.Big
	GasLimit     hexutil.Uint64
	Amount       *hexutil.Big
	Payload      hexutil.Bytes
	V            *hexutil.Big
	R            *hexutil.Big
	S            *hexutil.Big
}

func NewTransaction(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, action uint64, vote []byte, nickname []byte, asset *common.Address, assetInfo []byte, subAddress string) *Transaction {

	return newTransaction(nonce, &to, amount, gasLimit, gasPrice, data, action, vote, nickname, asset, assetInfo, subAddress, "")
}

func NewContractCreation(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, abi string, asset *common.Address) *Transaction {
	return newTransaction(nonce, nil, amount, gasLimit, gasPrice, data, ActionCreateContract, nil, nil, asset, nil, "", abi)
}

func newTransaction(nonce uint64, to *common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, action uint64, vote []byte, nickname []byte, asset *common.Address, assetInfo []byte, subAddress string, abi string) *Transaction {
	if len(data) > 0 {
		data = common.CopyBytes(data)
	}
	d := txdata{
		AccountNonce: nonce,
		Recipient:    to,
		Payload:      data,
		Amount:       new(big.Int),
		GasLimit:     gasLimit,
		Price:        new(big.Int),
		V:            new(big.Int),
		R:            new(big.Int),
		S:            new(big.Int),
		Vote:         vote,
		Nickname:     nickname,
		Action:       action,
		Asset:        asset,
		AssetInfo:    assetInfo,
		SubAddress:   subAddress,
		Abi:          abi,
	}
	if amount != nil {
		d.Amount.Set(amount)
	}
	if gasPrice != nil {
		d.Price.Set(gasPrice)
	}

	return &Transaction{data: d}
}

// ChainId returns which chain id this transaction was signed for (if at all)
func (tx *Transaction) ChainId() *big.Int {
	return deriveChainId(tx.data.V)
}

func (tx *Transaction) GetTransactionType() common.Address {
	switch tx.TxDataAction() {
	case ActionTrans:
		a := tx.Asset()
		if nil == a {
			return common.StringToAddress("aoa")
		} else {
			return *a
		}
	case ActionRegister:
		return common.StringToAddress(RegisterAgent)
	case ActionAddVote, ActionSubVote:
		return common.StringToAddress(VoteAgent)
	case ActionCreateContract:
		return common.StringToAddress(CreateContract)
	case ActionCallContract:
		return *tx.To()
	default:
		return common.StringToAddress(PublishAsset)
	}
}

// Protected returns whether the transaction is protected from replay protection.
//func (tx *Transaction) Protected() bool {
//	return isProtectedV(tx.data.V)
//}
//
//func isProtectedV(V *big.Int) bool {
//	if V.BitLen() <= 8 {
//		v := V.Uint64()
//		return v != 27 && v != 28
//	}
//	// anything not 27 or 28 are considered unprotected
//	return true
//}

// EncodeRLP implements rlp.Encoder
func (tx *Transaction) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &tx.data)
}

// DecodeRLP implements rlp.Decoder
func (tx *Transaction) DecodeRLP(s *rlp.Stream) error {
	_, size, _ := s.Kind()
	err := s.Decode(&tx.data)
	if err == nil {
		tx.size.Store(common.StorageSize(rlp.ListSize(size)))
	}

	return err
}

// MarshalJSON encodes the web3 RPC transaction format.
func (tx *Transaction) MarshalJSON() ([]byte, error) {
	hash := tx.Hash()
	data := tx.data
	data.Hash = &hash
	return data.MarshalJSON()
}

// UnmarshalJSON decodes the web3 RPC transaction format.
func (tx *Transaction) UnmarshalJSON(input []byte) error {
	var dec txdata
	if err := dec.UnmarshalJSON(input); err != nil {
		return err
	}
	var V byte
	chainID := deriveChainId(dec.V).Uint64()
	V = byte(dec.V.Uint64() - 35 - 2*chainID)

	if !crypto.ValidateSignatureValues(V, dec.R, dec.S, false) {
		return ErrInvalidSig
	}
	*tx = Transaction{data: dec}
	return nil
}

func (tx *Transaction) Nickname() []byte     { return tx.data.Nickname }
func (tx *Transaction) Vote() []byte         { return tx.data.Vote }
func (tx *Transaction) TxDataAction() uint64 { return tx.data.Action }
func (tx *Transaction) Data() []byte         { return common.CopyBytes(tx.data.Payload) }
func (tx *Transaction) Gas() uint64          { return tx.data.GasLimit }
func (tx *Transaction) GasPrice() *big.Int   { return new(big.Int).Set(tx.data.Price) }
func (tx *Transaction) Value() *big.Int      { return new(big.Int).Set(tx.data.Amount) }
func (tx *Transaction) Nonce() uint64        { return tx.data.AccountNonce }
func (tx *Transaction) CheckNonce() bool     { return true }
func (tx *Transaction) Asset() *common.Address {
	if tx.data.Asset == nil {
		return nil
	}
	a := *tx.data.Asset
	return &a
}
func (tx *Transaction) SubAddress() string { return tx.data.SubAddress }
func (tx *Transaction) AssetInfo() *AssetInfo {
	if nil == tx.data.AssetInfo {
		return nil
	}
	assetInfoBytes := tx.data.AssetInfo
	assetInfo, err := BytesToAssetInfo(assetInfoBytes)
	if err != nil {
		return nil
	}
	issuer := &common.Address{}
	if assetInfo.Issuer != nil {
		issuer.Set(*assetInfo.Issuer)
	}
	newai := AssetInfo{
		Supply: new(big.Int).Set(assetInfo.Supply),
		Name:   assetInfo.Name,
		Symbol: assetInfo.Symbol,
		Desc:   assetInfo.Desc,
		Issuer: issuer,
	}

	return &newai
}
func (tx *Transaction) Abi() string { return tx.data.Abi }

// To returns the recipient address of the transaction.
// It returns nil if the transaction is a contract creation.
func (tx *Transaction) To() *common.Address {
	if tx.data.Recipient == nil {
		return nil
	}
	to := *tx.data.Recipient
	return &to
}

// Hash hashes the RLP encoding of tx.
// It uniquely identifies the transaction.
func (tx *Transaction) Hash() common.Hash {
	if hash := tx.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	v := rlpHash(tx)
	tx.hash.Store(v)
	return v
}

func (tx *Transaction) Size() common.StorageSize {
	if size := tx.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, &tx.data)
	tx.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

func (tx *Transaction) GetIsContract() interface{} {
	return tx.isContract.Load()
}

func (tx *Transaction) SetIsContract(contract bool) {
	tx.isContract.Store(contract)
}

// AsMessage returns the transaction as a core.Message.
//
// AsMessage requires a signer to derive the sender.
//
// XXX Rename message to something less arbitrary?
func (tx *Transaction) AsMessage(s Signer) (Message, error) {
	vote := make([]Vote, 0)
	var err error
	if tx.data.Action == ActionAddVote || tx.data.Action == ActionSubVote {
		vote, err = BytesToVote(tx.data.Vote)
		if err != nil {
			return Message{}, err
		}
	}

	msg := Message{
		nonce:      tx.data.AccountNonce,
		gasLimit:   tx.data.GasLimit,
		gasPrice:   new(big.Int).Set(tx.data.Price),
		to:         tx.data.Recipient,
		amount:     tx.data.Amount,
		data:       tx.data.Payload,
		checkNonce: true,
		action:     tx.data.Action,
		vote:       vote,
		asset:      tx.data.Asset,
		subAddress: tx.data.SubAddress,
		abi:        tx.data.Abi,
	}
	if len(tx.data.AssetInfo) > 0 {
		assetInfo, err := BytesToAssetInfo(tx.data.AssetInfo)
		if err != nil {
			return msg, err
		}
		msg.assetInfo = assetInfo
	}
	//now := time.Now()
	msg.from, err = Sender(s, tx)
	return msg, err
}

// WithSignature returns a new transaction with the given signature.
// This signature needs to be formatted as described in the yellow paper (v+27).
func (tx *Transaction) WithSignature(signer Signer, sig []byte) (*Transaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}
	log.Infof("Transaction|WithSignature, V=%v",v.Int64())
	cpy := &Transaction{data: tx.data}
	cpy.data.R, cpy.data.S, cpy.data.V = r, s, v
	return cpy, nil
}

// AoaCost returns aoa required.
func (tx *Transaction) AoaCost() *big.Int {
	log.Debugf("Transaction|AoaCost, transaction=%v", tx.data)
	total := new(big.Int).Mul(tx.data.Price, new(big.Int).SetUint64(tx.data.GasLimit))
	// agent register cost
	if tx.data.Action == ActionRegister {
		registerCost := new(big.Int)
		registerCost.SetString(params.TxGasAgentCreation, 10)
		// 扣除100，精度是18位
		total.Add(total, registerCost)
		log.Infof("register agent cost, total=%v", total)
	} else if (tx.data.Asset == nil || (*tx.data.Asset == common.Address{})) && (tx.data.Amount != nil && tx.data.Amount.Sign() > 0) {
		total.Add(total, tx.data.Amount)
	}
	log.Infof("cost %v", total)
	return total
}

func (tx *Transaction) RawSignatureValues() (*big.Int, *big.Int, *big.Int) {
	return tx.data.V, tx.data.R, tx.data.S
}

func (tx *Transaction) String() string {
	var from, to string
	if tx.data.V != nil {
		// make a best guess about the signer and use that to derive
		// the sender.
		signer := deriveSigner(tx.data.V)
		if f, err := Sender(signer, tx); err != nil { // derive but don't cache
			from = "[invalid sender: invalid sig]"
		} else {
			from = fmt.Sprintf("%x", f[:])
		}

	} else {
		from = "[invalid sender: nil V field]"
	}

	if tx.data.Recipient == nil {
		to = "[contract creation]"
	} else {
		to = fmt.Sprintf("%x", tx.data.Recipient[:])
	}
	enc, _ := rlp.EncodeToBytes(&tx.data)
	var vote []Vote
	if tx.data.Vote != nil && len(tx.data.Vote) > 0 {
		json.Unmarshal(tx.data.Vote, &vote)
	}
	var asset = "nil"
	if nil != tx.data.Asset {
		asset = fmt.Sprintf("%x", tx.data.Asset[:])
	}
	var aiStr = "nil"
	if nil != tx.data.AssetInfo {
		b, err := json.Marshal(tx.data.AssetInfo)
		if nil != err {
			aiStr = "[Error when marshalling]"
		} else {
			aiStr = string(b)
		}

	}
	return fmt.Sprintf(`
	TX(%x)
	Contract: %v
	From:     %s
	To:       %s
	Nonce:    %v
	GasPrice: %#x
	GasLimit  %#x
	Value:    %#x
	Data:     0x%x
	V:        %#x
	R:        %#x
	S:        %#x
	Hex:      %x
    Action:   %#x
    Vote:     %v
    Nickname: %s 
	Asset:		%s
	AssetInfo:  %s
	SubAddress: %s
`,
		tx.Hash(),
		tx.data.Recipient == nil,
		from,
		to,

		tx.data.AccountNonce,
		tx.data.Price,
		tx.data.GasLimit,
		tx.data.Amount,
		tx.data.Payload,
		tx.data.V,
		tx.data.R,
		tx.data.S,
		enc,
		tx.data.Action,
		vote,
		string(tx.data.Nickname),
		asset,
		aiStr,
		tx.data.SubAddress,
	)
}

// Transactions is a Transaction slice type for basic sorting.
type Transactions []*Transaction

// Len returns the length of s.
func (s Transactions) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s.
func (s Transactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements Rlpable and returns the i'th element of s in rlp.
func (s Transactions) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i])
	return enc
}

// TxDifference returns a new set t which is the difference between a to b.
func TxDifference(a, b Transactions) (keep Transactions) {
	keep = make(Transactions, 0, len(a))

	remove := make(map[common.Hash]struct{})
	for _, tx := range b {
		remove[tx.Hash()] = struct{}{}
	}

	for _, tx := range a {
		if _, ok := remove[tx.Hash()]; !ok {
			keep = append(keep, tx)
		}
	}

	return keep
}

// TxByNonce implements the sort interface to allow sorting a list of transactions
// by their nonces. This is usually only useful for sorting transactions from a
// single account, otherwise a nonce comparison doesn't make much sense.
type TxByNonce Transactions

func (s TxByNonce) Len() int           { return len(s) }
func (s TxByNonce) Less(i, j int) bool { return s[i].data.AccountNonce < s[j].data.AccountNonce }
func (s TxByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// TxByPrice implements both the sort and the heap interface, making it useful
// for all at once sorting as well as individually adding and removing elements.
type TxByPrice Transactions

func (s TxByPrice) Len() int           { return len(s) }
func (s TxByPrice) Less(i, j int) bool { return s[i].data.Price.Cmp(s[j].data.Price) > 0 }
func (s TxByPrice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (s *TxByPrice) Push(x interface{}) {
	*s = append(*s, x.(*Transaction))
}

func (s *TxByPrice) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

func (s *TxByPrice) Get(hash common.Hash) (int, *Transaction) {
	for index, tx := range *s {
		if hash == tx.Hash() {
			return index, tx
		}
	}
	return -1, nil
}

func (s *TxByPrice) Remove(index int) common.Hash {
	removeTx := (*s)[index]
	*s = append((*s)[:index], (*s)[index+1:]...)
	return removeTx.Hash()
}

func SortByPriceAndNonce(signer Signer, txList TxByPrice) Transactions {
	txs := make(map[common.Address]chan *Transaction, len(txList))
	for _, tx := range txList {
		from, _ := Sender(signer, tx)
		if len(txs[from]) == 0 {
			txs[from] = make(chan *Transaction, len(txList))
		}
		txs[from] <- tx
	}
	var sortResults []<-chan *Transaction
	for _, txChannel := range txs {
		sortResults = append(sortResults, inMemSort(txChannel))
		close(txChannel)
	}
	finalSort := mergeNTxs(sortResults...)
	result := make(Transactions, 0)
	for tx := range finalSort {
		result = append(result, tx)
	}
	return result
}

func inMemSort(in <-chan *Transaction) <-chan *Transaction {
	out := make(chan *Transaction)
	go func() {
		txSortByNonce := make(TxByNonce, 0)
		for tx := range in {
			txSortByNonce = append(txSortByNonce, tx)
		}
		sort.Sort(txSortByNonce)
		for _, tx := range txSortByNonce {
			//fmt.Print(tx.Nonce(), " ")
			out <- tx
		}
		close(out)
	}()
	return out
}

func mergeNTxs(txs ...<-chan *Transaction) <-chan *Transaction {
	if len(txs) == 1 {
		return txs[0]
	}
	m := len(txs) / 2
	return mergeTx(mergeNTxs(txs[:m]...), mergeNTxs(txs[m:]...))
}

func mergeTx(in1, in2 <-chan *Transaction) <-chan *Transaction {
	out := make(chan *Transaction)
	go func() {
		v1, ok1 := <-in1
		v2, ok2 := <-in2
		for ok1 || ok2 {
			if !ok2 || (ok1 && v1.GasPrice().Cmp(v2.GasPrice()) >= 0) {
				out <- v1
				v1, ok1 = <-in1
			} else {
				out <- v2
				v2, ok2 = <-in2
			}
		}
		close(out)
	}()
	return out
}

// TransactionsByPriceAndNonce represents a set of transactions that can return
// transactions in a profit-maximizing sorted order, while supporting removing
// entire batches of transactions for non-executable accounts.
type TransactionsByPriceAndNonce struct {
	txs    map[common.Address]Transactions // Per account nonce-sorted list of transactions
	heads  TxByPrice                       // Next transaction for each unique account (price heap)
	signer Signer                          // Signer for the set of transactions
}

// NewTransactionsByPriceAndNonce creates a transaction set that can retrieve
// price sorted transactions in a nonce-honouring way.
//
// Note, the input map is reowned so the caller should not interact any more with
// if after providing it to the constructor.
func NewTransactionsByPriceAndNonce(signer Signer, txs map[common.Address]Transactions) *TransactionsByPriceAndNonce {
	// Initialize a price based heap with the head transactions
	heads := make(TxByPrice, 0, len(txs))
	for _, accTxs := range txs {
		heads = append(heads, accTxs[0])
		// Ensure the sender address is from the signer
		acc, _ := Sender(signer, accTxs[0])
		txs[acc] = accTxs[1:]
	}
	heap.Init(&heads)

	// Assemble and return the transaction set
	return &TransactionsByPriceAndNonce{
		txs:    txs,
		heads:  heads,
		signer: signer,
	}
}

func NewTransactionsByPriceAndNonce2(signer Signer, price TxByPrice) *TransactionsByPriceAndNonce {
	txByNonce := new(TxByNonce)
	*txByNonce = append(*txByNonce, price...)
	sort.Sort(txByNonce)
	txs := make(map[common.Address]Transactions, 0)
	for _, tx := range *txByNonce {
		from, _ := Sender(signer, tx)
		txs[from] = append(txs[from], tx)
	}
	return &TransactionsByPriceAndNonce{
		txs:    txs,
		heads:  price,
		signer: signer,
	}
}

// Peek returns the next transaction by price.
func (t *TransactionsByPriceAndNonce) Peek() *Transaction {
	if len(t.heads) == 0 {
		return nil
	}
	return t.heads[0]
}

// Shift replaces the current best head with the next one from the same account.
func (t *TransactionsByPriceAndNonce) Shift() {
	acc, _ := Sender(t.signer, t.heads[0])
	if txs, ok := t.txs[acc]; ok && len(txs) > 0 {
		t.heads[0], t.txs[acc] = txs[0], txs[1:]
		heap.Fix(&t.heads, 0)
	} else {
		heap.Pop(&t.heads)
	}
}

// Pop removes the best transaction, *not* replacing it with the next one from
// the same account. This should be used when a transaction cannot be executed
// and hence all subsequent ones should be discarded from the same account.
func (t *TransactionsByPriceAndNonce) Pop() {
	heap.Pop(&t.heads)
}

// Message is a fully derived transaction and implements core.Message
//
// NOTE: In a future PR this will be removed.
type Message struct {
	to         *common.Address
	from       common.Address
	nonce      uint64
	amount     *big.Int
	gasLimit   uint64
	gasPrice   *big.Int
	data       []byte
	checkNonce bool
	action     uint64
	vote       []Vote
	asset      *common.Address
	assetInfo  *AssetInfo
	subAddress string
	abi        string
}

func NewMessage(from common.Address, to *common.Address, nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte, checkNonce bool, action uint64, vote []Vote, asset *common.Address, assetInfo *AssetInfo, subAddress string, abi string) Message {
	return Message{
		from:       from,
		to:         to,
		nonce:      nonce,
		amount:     amount,
		gasLimit:   gasLimit,
		gasPrice:   gasPrice,
		data:       data,
		checkNonce: checkNonce,
		action:     action,
		vote:       vote,
		asset:      asset,
		assetInfo:  assetInfo,
		subAddress: subAddress,
		abi:        abi,
	}
}

func (m Message) From() common.Address   { return m.from }
func (m Message) To() *common.Address    { return m.to }
func (m Message) GasPrice() *big.Int     { return m.gasPrice }
func (m Message) Value() *big.Int        { return m.amount }
func (m Message) Gas() uint64            { return m.gasLimit }
func (m Message) Nonce() uint64          { return m.nonce }
func (m Message) Data() []byte           { return m.data }
func (m Message) CheckNonce() bool       { return m.checkNonce }
func (m Message) Action() uint64         { return m.action }
func (m Message) Vote() []Vote           { return m.vote }
func (m Message) Asset() *common.Address { return m.asset }
func (m Message) SubAddress() string     { return m.subAddress }
func (m Message) AssetInfo() AssetInfo {
	if nil != m.assetInfo {
		return *m.assetInfo
	}
	return AssetInfo{}
}
func (m Message) Abi() string { return m.abi }
