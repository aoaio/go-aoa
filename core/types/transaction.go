package types

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync/atomic"

	"encoding/json"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"sort"
)

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

func deriveSigner(V *big.Int) Signer {
	if V.Sign() != 0 {
		return NewAuroraSigner(deriveChainId(V))
	}
	return NewAuroraSigner(big.NewInt(1))
}

type Transaction struct {
	data txdata

	hash       atomic.Value
	size       atomic.Value
	from       atomic.Value
	isContract atomic.Value
}

type Vote struct {
	Candidate *common.Address `json:"candidate"`
	Operation uint            `json:"operation"` 
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
	Recipient    *common.Address `json:"to"       rlp:"nil"` 
	Amount       *big.Int        `json:"value"    gencodec:"required"`
	Payload      []byte          `json:"input"    gencodec:"required"`

	Action   uint64 `json:"action"  gencodec:"required"` 
	Vote     []byte `json:"vote" rlp:"nil"`
	Nickname []byte `json:"nickname" rlp:"nil"`

	Asset *common.Address `json:"asset,omitempty" rlp:"nil"`

	AssetInfo []byte `json:"assetInfo,omitempty" rlp:"nil"`

	SubAddress string `json:"subAddress,omitempty" rlp:"nil"`

	Abi string `json:"abi,omitempty" rlp:"nil"`

	V *big.Int `json:"v" gencodec:"required"` 
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

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

func (tx *Transaction) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &tx.data)
}

func (tx *Transaction) DecodeRLP(s *rlp.Stream) error {
	_, size, _ := s.Kind()
	err := s.Decode(&tx.data)
	if err == nil {
		tx.size.Store(common.StorageSize(rlp.ListSize(size)))
	}

	return err
}

func (tx *Transaction) MarshalJSON() ([]byte, error) {
	hash := tx.Hash()
	data := tx.data
	data.Hash = &hash
	return data.MarshalJSON()
}

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

func (tx *Transaction) To() *common.Address {
	if tx.data.Recipient == nil {
		return nil
	}
	to := *tx.data.Recipient
	return &to
}

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

	msg.from, err = Sender(s, tx)

	return msg, err
}

func (tx *Transaction) WithSignature(signer Signer, sig []byte) (*Transaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}
	log.Debug("Transaction|WithSignature", "v", v.Int64())
	cpy := &Transaction{data: tx.data}
	cpy.data.R, cpy.data.S, cpy.data.V = r, s, v
	return cpy, nil
}

func (tx *Transaction) AoaCost() *big.Int {
	log.Info("Transaction|AoaCost,", "transaction", tx.data)
	total := new(big.Int).Mul(tx.data.Price, new(big.Int).SetUint64(tx.data.GasLimit))

	if tx.data.Action == ActionRegister {
		registerCost := new(big.Int)
		registerCost.SetString(params.TxGasAgentCreation, 10)

		total.Add(total, registerCost)
		log.Info("register agent cost", "total", total)
	} else if (tx.data.Asset == nil || (*tx.data.Asset == common.Address{})) && (tx.data.Amount != nil && tx.data.Amount.Sign() > 0) {
		total.Add(total, tx.data.Amount)
	}
	log.Debug("cost", "total", total)
	return total
}

func (tx *Transaction) RawSignatureValues() (*big.Int, *big.Int, *big.Int) {
	return tx.data.V, tx.data.R, tx.data.S
}

func (tx *Transaction) String() string {
	var from, to string
	if tx.data.V != nil {

		signer := deriveSigner(tx.data.V)
		if f, err := Sender(signer, tx); err != nil { 
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

type Transactions []*Transaction

func (s Transactions) Len() int { return len(s) }

func (s Transactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s Transactions) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i])
	return enc
}

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

type TxByNonce Transactions

func (s TxByNonce) Len() int           { return len(s) }
func (s TxByNonce) Less(i, j int) bool { return s[i].data.AccountNonce < s[j].data.AccountNonce }
func (s TxByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

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

type TransactionsByPriceAndNonce struct {
	txs    map[common.Address]Transactions 
	heads  TxByPrice                       
	signer Signer                          
}

func NewTransactionsByPriceAndNonce(signer Signer, txs map[common.Address]Transactions) *TransactionsByPriceAndNonce {

	heads := make(TxByPrice, 0, len(txs))
	for _, accTxs := range txs {
		heads = append(heads, accTxs[0])

		acc, _ := Sender(signer, accTxs[0])
		txs[acc] = accTxs[1:]
	}
	heap.Init(&heads)

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

func (t *TransactionsByPriceAndNonce) Peek() *Transaction {
	if len(t.heads) == 0 {
		return nil
	}
	return t.heads[0]
}

func (t *TransactionsByPriceAndNonce) Shift() {
	acc, _ := Sender(t.signer, t.heads[0])
	if txs, ok := t.txs[acc]; ok && len(txs) > 0 {
		t.heads[0], t.txs[acc] = txs[0], txs[1:]
		heap.Fix(&t.heads, 0)
	} else {
		heap.Pop(&t.heads)
	}
}

func (t *TransactionsByPriceAndNonce) Pop() {
	heap.Pop(&t.heads)
}

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
