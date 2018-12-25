package core

import (
	"container/heap"
	"math"
	"math/big"
	"sort"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/log"
	"strings"
	"github.com/Aurorachain/go-Aurora/core/state"
)

type nonceHeap []uint64

func (h nonceHeap) Len() int           { return len(h) }
func (h nonceHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h nonceHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nonceHeap) Push(x interface{}) {
	*h = append(*h, x.(uint64))
}

func (h *nonceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type txSortedMap struct {
	items map[uint64]*types.Transaction
	index *nonceHeap
	cache types.Transactions
}

func newTxSortedMap() *txSortedMap {
	return &txSortedMap{
		items: make(map[uint64]*types.Transaction),
		index: new(nonceHeap),
	}
}

func (m *txSortedMap) Get(nonce uint64) *types.Transaction {
	return m.items[nonce]
}

func (m *txSortedMap) Put(tx *types.Transaction) {
	nonce := tx.Nonce()
	if m.items[nonce] == nil {
		heap.Push(m.index, nonce)
	}
	m.items[nonce], m.cache = tx, nil
}

func (m *txSortedMap) Forward(threshold uint64) types.Transactions {
	var removed types.Transactions

	for m.index.Len() > 0 && (*m.index)[0] < threshold {
		nonce := heap.Pop(m.index).(uint64)
		removed = append(removed, m.items[nonce])
		delete(m.items, nonce)
	}

	if m.cache != nil {
		m.cache = m.cache[len(removed):]
	}
	return removed
}

func (m *txSortedMap) Filter(filter func(*types.Transaction) bool) types.Transactions {
	var removed types.Transactions

	for nonce, tx := range m.items {
		if filter(tx) {
			removed = append(removed, tx)
			delete(m.items, nonce)
		}
	}

	if len(removed) > 0 {
		*m.index = make([]uint64, 0, len(m.items))
		for nonce := range m.items {
			*m.index = append(*m.index, nonce)
		}
		heap.Init(m.index)

		m.cache = nil
	}
	return removed
}

func (m *txSortedMap) Cap(threshold int) types.Transactions {

	if len(m.items) <= threshold {
		return nil
	}

	var drops types.Transactions

	sort.Sort(*m.index)
	for size := len(m.items); size > threshold; size-- {
		drops = append(drops, m.items[(*m.index)[size-1]])
		delete(m.items, (*m.index)[size-1])
	}
	*m.index = (*m.index)[:threshold]
	heap.Init(m.index)

	if m.cache != nil {
		m.cache = m.cache[:len(m.cache)-len(drops)]
	}
	return drops
}

func (m *txSortedMap) Remove(nonce uint64) bool {

	_, ok := m.items[nonce]
	if !ok {
		return false
	}

	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i] == nonce {
			heap.Remove(m.index, i)
			break
		}
	}
	delete(m.items, nonce)
	m.cache = nil

	return true
}

func (m *txSortedMap) Ready(start uint64) types.Transactions {

	if m.index.Len() == 0 || (*m.index)[0] > start {
		return nil
	}

	var ready types.Transactions
	for next := (*m.index)[0]; m.index.Len() > 0 && (*m.index)[0] == next; next++ {
		ready = append(ready, m.items[next])
		delete(m.items, next)
		heap.Pop(m.index)
	}
	m.cache = nil

	return ready
}

func (m *txSortedMap) Len() int {
	return len(m.items)
}

func (m *txSortedMap) Flatten() types.Transactions {

	if m.cache == nil {
		m.cache = make(types.Transactions, 0, len(m.items))
		for _, tx := range m.items {
			m.cache = append(m.cache, tx)
		}
		sort.Sort(types.TxByNonce(m.cache))
	}

	txs := make(types.Transactions, len(m.cache))
	copy(txs, m.cache)
	return txs
}

type txList struct {
	strict bool
	txs    *txSortedMap

	costcap *big.Int
	gascap  uint64
}

func newTxList(strict bool) *txList {
	return &txList{
		strict:  strict,
		txs:     newTxSortedMap(),
		costcap: new(big.Int),
	}
}

func (l *txList) Overlaps(tx *types.Transaction) bool {
	return l.txs.Get(tx.Nonce()) != nil
}

func (l *txList) Add(tx *types.Transaction, priceBump uint64) (bool, *types.Transaction) {

	old := l.txs.Get(tx.Nonce())
	if old != nil {
		threshold := new(big.Int).Div(new(big.Int).Mul(old.GasPrice(), big.NewInt(100+int64(priceBump))), big.NewInt(100))

		if old.GasPrice().Cmp(tx.GasPrice()) >= 0 || threshold.Cmp(tx.GasPrice()) > 0 {
			return false, nil
		}
	}

	l.txs.Put(tx)
	cost := tx.AoaCost()
	if l.costcap.Cmp(cost) < 0 {
		l.costcap = cost
	}
	if gas := tx.Gas(); l.gascap < gas {
		l.gascap = gas
	}
	return true, old
}

func (l *txList) Forward(threshold uint64) types.Transactions {
	return l.txs.Forward(threshold)
}

func (l *txList) Filter(costLimit *big.Int, gasLimit uint64) (types.Transactions, types.Transactions) {

	if l.costcap.Cmp(costLimit) <= 0 && l.gascap <= gasLimit {
		return nil, nil
	}
	l.costcap = new(big.Int).Set(costLimit)
	l.gascap = gasLimit

	removed := l.txs.Filter(func(tx *types.Transaction) bool { return tx.AoaCost().Cmp(costLimit) > 0 || tx.Gas() > gasLimit })

	var invalids types.Transactions

	if l.strict && len(removed) > 0 {
		lowest := uint64(math.MaxUint64)
		for _, tx := range removed {
			if nonce := tx.Nonce(); lowest > nonce {
				lowest = nonce
			}
		}
		invalids = l.txs.Filter(func(tx *types.Transaction) bool { return tx.Nonce() > lowest })
	}
	return removed, invalids
}

func (l *txList) Cap(threshold int) types.Transactions {
	return l.txs.Cap(threshold)
}

func (l *txList) Remove(tx *types.Transaction) (bool, types.Transactions) {

	nonce := tx.Nonce()
	if removed := l.txs.Remove(nonce); !removed {
		return false, nil
	}

	if l.strict {
		return true, l.txs.Filter(func(tx *types.Transaction) bool { return tx.Nonce() > nonce })
	}
	return true, nil
}

func (l *txList) Ready(start uint64) types.Transactions {
	return l.txs.Ready(start)
}

func (l *txList) Len() int {
	return l.txs.Len()
}

func (l *txList) Empty() bool {
	return l.Len() == 0
}

func (l *txList) Flatten() types.Transactions {
	return l.txs.Flatten()
}

type priceHeap []*types.Transaction

func (h priceHeap) Len() int           { return len(h) }
func (h priceHeap) Less(i, j int) bool { return h[i].GasPrice().Cmp(h[j].GasPrice()) < 0 }
func (h priceHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *priceHeap) Push(x interface{}) {
	*h = append(*h, x.(*types.Transaction))
}

func (h *priceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h *priceHeap) Get(hash common.Hash) (int, *types.Transaction) {
	for index, tx := range *h {
		if hash == tx.Hash() {
			return index, tx
		}
	}
	return -1, nil
}

type txPricedList struct {
	all    *map[common.Hash]*types.Transaction
	items  *priceHeap
	stales int
}

func newTxPricedList(all *map[common.Hash]*types.Transaction) *txPricedList {
	return &txPricedList{
		all:   all,
		items: new(priceHeap),
	}
}

func (l *txPricedList) Put(tx *types.Transaction) {
	heap.Push(l.items, tx)
}

func (l *txPricedList) Removed() {

	l.stales++
	if l.stales <= len(*l.items)/4 {
		return
	}

	reheap := make(priceHeap, 0, len(*l.all))

	l.stales, l.items = 0, &reheap
	for _, tx := range *l.all {
		*l.items = append(*l.items, tx)
	}
	heap.Init(l.items)
}

func (l *txPricedList) Cap(threshold *big.Int, local *accountSet) types.Transactions {
	drop := make(types.Transactions, 0, 128)
	save := make(types.Transactions, 0, 64)

	for len(*l.items) > 0 {

		tx := heap.Pop(l.items).(*types.Transaction)
		if _, ok := (*l.all)[tx.Hash()]; !ok {
			l.stales--
			continue
		}

		if tx.GasPrice().Cmp(threshold) >= 0 {
			save = append(save, tx)
			break
		}

		if local.containsTx(tx) {
			save = append(save, tx)
		} else {
			drop = append(drop, tx)
		}
	}
	for _, tx := range save {
		heap.Push(l.items, tx)
	}
	return drop
}

func (l *txPricedList) Underpriced(tx *types.Transaction, local *accountSet) bool {

	if local.containsTx(tx) {
		return false
	}

	for len(*l.items) > 0 {
		head := []*types.Transaction(*l.items)[0]
		if _, ok := (*l.all)[head.Hash()]; !ok {
			l.stales--
			heap.Pop(l.items)
			continue
		}
		break
	}

	if len(*l.items) == 0 {
		log.Error("Pricing query for empty pool")
		return false
	}
	cheapest := []*types.Transaction(*l.items)[0]
	return cheapest.GasPrice().Cmp(tx.GasPrice()) >= 0
}

func (l *txPricedList) Discard(count int, local *accountSet) types.Transactions {
	drop := make(types.Transactions, 0, count)
	save := make(types.Transactions, 0, 64)

	for len(*l.items) > 0 && count > 0 {

		tx := heap.Pop(l.items).(*types.Transaction)
		if _, ok := (*l.all)[tx.Hash()]; !ok {
			l.stales--
			continue
		}

		if local.containsTx(tx) {
			save = append(save, tx)
		} else {
			drop = append(drop, tx)
			count--
		}
	}
	for _, tx := range save {
		heap.Push(l.items, tx)
	}
	return drop
}

type txPoolSorter []*KV

func (t *txPoolSorter) TxNum() int {
	count := 0
	for _, kv := range *t {
		count += kv.GetValueLen()
	}
	return count
}

func (t *txPoolSorter) ContractNum() int {
	count := 0
	for _, kv := range *t {
		if kv.contract {
			count += kv.GetValueLen()
		}
	}
	return count
}

func (t *txPoolSorter) GetTransactionList() types.TxByPrice {
	res := make(types.TxByPrice, 0)
	for _, kv := range *t {
		value := kv.value
		for _, tx := range *value {
			heap.Push(&res, tx)
		}
	}
	return res
}

type KV struct {
	key      common.Address
	value    *types.TxByPrice
	contract bool
}

func (kv *KV) GetKey() string {
	return strings.ToLower(kv.key.Hex())
}
func (kv *KV) GetValueLen() int {
	return kv.value.Len()
}
func (t *txPoolSorter) Push(x interface{}) {
	*t = append(*t, x.(*KV))
}

func (t *txPoolSorter) Pop() interface{} {
	old := *t
	n := len(old)
	x := old[n-1]
	*t = old[0 : n-1]
	return x
}

func (t txPoolSorter) Len() int {
	return len(t)
}

func (t txPoolSorter) Less(i, j int) bool {
	return len(*t[i].value) > len(*t[j].value)
}

func (t txPoolSorter) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

func (t *txPoolSorter) Get(address common.Address) (int, *types.TxByPrice) {
	for index, txList := range *t {
		if address == txList.key {
			return index, txList.value
		}
	}
	return -1, nil
}

func (t *txPoolSorter) Remove(index int) {
	heap.Remove(t, index)
}

type txTypeList struct {
	all   *map[common.Hash]*types.Transaction
	items *txPoolSorter

}

func newTxTypeList(all *map[common.Hash]*types.Transaction) *txTypeList {
	return &txTypeList{
		all:   all,
		items: new(txPoolSorter),
	}
}

func (tList *txTypeList) Put(tx *types.Transaction, db *state.StateDB) {
	txType := tx.GetTransactionType()
	index, txList := tList.items.Get(txType)
	isContract := IsContractTransaction(tx, db)
	tx.SetIsContract(isContract)
	if -1 == index {
		newTxList := new(types.TxByPrice)
		heap.Push(newTxList, tx)
		kv := &KV{
			key:      txType,
			value:    newTxList,
			contract: isContract,
		}
		heap.Push(tList.items, kv)
		return
	}
	heap.Push(txList, tx)
}

func (tList *txTypeList) Get(txType common.Address) (int, *types.TxByPrice) {
	index, txList := tList.items.Get(txType)
	return index, txList
}

func (tList *txTypeList) Underpriced(tx *types.Transaction) bool {
	txType := tx.GetTransactionType()
	_, txList := tList.Get(txType)
	sort.Sort(txList)
	for len(*txList) > 0 {
		head := (*txList)[len(*txList)-1]
		if _, ok := (*tList.all)[head.Hash()]; !ok {
			txList.Remove(len(*txList) - 1)
			continue
		}
		break
	}
	if len(*tList.items) == 0 {
		log.Error("Pricing query for empty pool")
		return false
	}
	cheapest := (*txList)[len(*txList)-1]
	return cheapest.GasPrice().Cmp(tx.GasPrice()) >= 0
}

func (tList *txTypeList) Cap(threshold *big.Int, local *accountSet, db *state.StateDB) types.Transactions {
	drop := make(types.Transactions, 0, 128)
	save := make(types.Transactions, 0, 64)

	txList := tList.items.GetTransactionList()
	for len(txList) > 0 {

		tx := heap.Pop(&txList).(*types.Transaction)
		if _, ok := (*tList.all)[tx.Hash()]; !ok {
			continue
		}

		if tx.GasPrice().Cmp(threshold) >= 0 {
			save = append(save, tx)
			break
		}

		if local.containsTx(tx) {
			save = append(save, tx)
		} else {
			drop = append(drop, tx)
		}
	}
	for _, tx := range save {
		tList.Put(tx, db)
	}
	return drop
}

func (tList *txTypeList) RemoveByHash(txType common.Address, hash common.Hash) {
	i, txList := tList.items.Get(txType)
	index, _ := txList.Get(hash)
	heap.Remove(txList, index)
	if len(*txList) == 0 {
		heap.Remove(tList.items, i)
	}
}

func (tList *txTypeList) RemoveTx() common.Hash {
	txList := (*tList.items)[0].value
	sort.Sort(txList)
	removeTx := (*txList)[len(*txList)-1]
	return removeTx.Hash()
}
