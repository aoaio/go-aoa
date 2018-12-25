package aoaapi

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"bytes"
	"github.com/Aurorachain/go-Aurora/accounts"
	"github.com/Aurorachain/go-Aurora/accounts/keystore"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/common/math"
	"github.com/Aurorachain/go-Aurora/common/ntp"
	"github.com/Aurorachain/go-Aurora/core"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/p2p"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/rpc"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	aa "github.com/Aurorachain/go-Aurora/accounts/walletType"
	"regexp"
	"sort"
)

const (
	defaultGasPrice    = 4 * params.Shannon
	subAddressLength   = 32
	strictAddresLength = 50
)

type PublicAuroraAPI struct {
	b Backend
}

func NewPublicAuroraAPI(b Backend) *PublicAuroraAPI {
	return &PublicAuroraAPI{b}
}

func (s *PublicAuroraAPI) GasPrice(ctx context.Context) (*big.Int, error) {
	return s.b.SuggestPrice(ctx)
}

func (s *PublicAuroraAPI) ProtocolVersion() hexutil.Uint {
	return hexutil.Uint(s.b.ProtocolVersion())
}

func (s *PublicAuroraAPI) Syncing() (interface{}, error) {
	progress := s.b.Downloader().Progress()

	if progress.CurrentBlock >= progress.HighestBlock {
		return false, nil
	}

	return map[string]interface{}{
		"startingBlock": hexutil.Uint64(progress.StartingBlock),
		"currentBlock":  hexutil.Uint64(progress.CurrentBlock),
		"highestBlock":  hexutil.Uint64(progress.HighestBlock),
		"pulledStates":  hexutil.Uint64(progress.PulledStates),
		"knownStates":   hexutil.Uint64(progress.KnownStates),
	}, nil
}

type PublicTxPoolAPI struct {
	b Backend
}

func NewPublicTxPoolAPI(b Backend) *PublicTxPoolAPI {
	return &PublicTxPoolAPI{b}
}

func (s *PublicTxPoolAPI) Content() map[string]map[string]map[string]*RPCTransaction {
	content := map[string]map[string]map[string]*RPCTransaction{
		"pending": make(map[string]map[string]*RPCTransaction),
		"queued":  make(map[string]map[string]*RPCTransaction),
	}
	pending, queue := s.b.TxPoolContent()

	for account, txs := range pending {
		dump := make(map[string]*RPCTransaction)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = newRPCPendingTransaction(tx)
		}
		content["pending"][account.Hex()] = dump
	}

	for account, txs := range queue {
		dump := make(map[string]*RPCTransaction)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = newRPCPendingTransaction(tx)
		}
		content["queued"][account.Hex()] = dump
	}
	return content
}

func (s *PublicTxPoolAPI) Status() map[string]hexutil.Uint {
	pending, queue := s.b.Stats()
	return map[string]hexutil.Uint{
		"pending": hexutil.Uint(pending),
		"queued":  hexutil.Uint(queue),
	}
}

func (s *PublicTxPoolAPI) Inspect() map[string]map[string]map[string]string {
	content := map[string]map[string]map[string]string{
		"pending": make(map[string]map[string]string),
		"queued":  make(map[string]map[string]string),
	}
	pending, queue := s.b.TxPoolContent()

	var format = func(tx *types.Transaction) string {
		if to := tx.To(); to != nil {
			return fmt.Sprintf("%s: %v wei + %v gas × %v wei", tx.To().Hex(), tx.Value(), tx.Gas(), tx.GasPrice())
		}
		return fmt.Sprintf("contract creation: %v wei + %v gas × %v wei", tx.Value(), tx.Gas(), tx.GasPrice())
	}

	for account, txs := range pending {
		dump := make(map[string]string)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = format(tx)
		}
		content["pending"][account.Hex()] = dump
	}

	for account, txs := range queue {
		dump := make(map[string]string)
		for _, tx := range txs {
			dump[fmt.Sprintf("%d", tx.Nonce())] = format(tx)
		}
		content["queued"][account.Hex()] = dump
	}
	return content
}

type PublicAccountAPI struct {
	am *accounts.Manager
}

func NewPublicAccountAPI(am *accounts.Manager) *PublicAccountAPI {
	return &PublicAccountAPI{am: am}
}

func (s *PublicAccountAPI) Accounts() []common.Address {
	addresses := make([]common.Address, 0)
	for _, wallet := range s.am.Wallets() {
		for _, account := range wallet.Accounts() {
			addresses = append(addresses, account.Address)
		}
	}
	return addresses
}

type PrivateAccountAPI struct {
	am        *accounts.Manager
	nonceLock *AddrLocker
	b         Backend
}

func NewPrivateAccountAPI(b Backend, nonceLock *AddrLocker) *PrivateAccountAPI {
	return &PrivateAccountAPI{
		am:        b.AccountManager(),
		nonceLock: nonceLock,
		b:         b,
	}
}

func (s *PrivateAccountAPI) ListAccounts() []common.Address {
	addresses := make([]common.Address, 0)
	for _, wallet := range s.am.Wallets() {
		for _, account := range wallet.Accounts() {
			addresses = append(addresses, account.Address)
		}
	}
	return addresses
}

type rawWallet struct {
	URL      string             `json:"url"`
	Status   string             `json:"status"`
	Failure  string             `json:"failure,omitempty"`
	Accounts []accounts.Account `json:"accounts,omitempty"`
}

func (s *PrivateAccountAPI) ListWallets() []rawWallet {
	wallets := make([]rawWallet, 0)
	for _, wallet := range s.am.Wallets() {
		status, failure := wallet.Status()

		raw := rawWallet{
			URL:      wallet.URL().String(),
			Status:   status,
			Accounts: wallet.Accounts(),
		}
		if failure != nil {
			raw.Failure = failure.Error()
		}
		wallets = append(wallets, raw)
	}
	return wallets
}

func (s *PrivateAccountAPI) OpenWallet(url string, passphrase *string) error {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return err
	}
	pass := ""
	if passphrase != nil {
		pass = *passphrase
	}
	return wallet.Open(pass)
}

func (s *PrivateAccountAPI) DeriveAccount(url string, path string, pin *bool) (accounts.Account, error) {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return accounts.Account{}, err
	}
	derivPath, err := accounts.ParseDerivationPath(path)
	if err != nil {
		return accounts.Account{}, err
	}
	if pin == nil {
		pin = new(bool)
	}
	return wallet.Derive(derivPath, *pin)
}

func (s *PrivateAccountAPI) NewAccount(password string) (common.Address, error) {
	acc, err := fetchKeystore(s.am).NewAccount(password)
	if err == nil {
		return acc.Address, nil
	}
	return common.Address{}, err
}

func fetchKeystore(am *accounts.Manager) *keystore.KeyStore {
	return am.Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
}

func (s *PrivateAccountAPI) ImportRawKey(privkey string, password string) (common.Address, error) {
	key, err := crypto.HexToECDSA(privkey)
	if err != nil {
		return common.Address{}, err
	}
	acc, err := fetchKeystore(s.am).ImportECDSA(key, password)
	return acc.Address, err
}

func (s *PrivateAccountAPI) UnlockAccount(addr common.Address, password string, duration *uint64) (bool, error) {
	const max = uint64(time.Duration(math.MaxInt64) / time.Second)
	var d time.Duration
	if duration == nil {
		d = 300 * time.Second
	} else if *duration > max {
		return false, errors.New("unlock duration too large")
	} else {
		d = time.Duration(*duration) * time.Second
	}
	err := fetchKeystore(s.am).TimedUnlock(accounts.Account{Address: addr}, password, d)
	return err == nil, err
}

func (s *PrivateAccountAPI) LockAccount(addr common.Address) bool {
	return fetchKeystore(s.am).Lock(addr) == nil
}

func (s *PrivateAccountAPI) ActivateDelegate(addr common.Address, password string) (bool, error) {
	if err := ntp.CheckLocalTimeIsNtp(); err != nil {
		return false, err
	}
	delegateList, err := s.b.GetDelegatePoll(s.b.CurrentBlock())
	if err != nil {
		return false, err
	}
	if _, ok := (*delegateList)[addr]; !ok {
		return false, errors.New(strings.ToLower(addr.Hex()) + " is not delegate")
	}
	account := accounts.Account{Address: addr}
	keyJson, err := fetchKeystore(s.am).Export(account, password, password)
	if err != nil {
		return false, err
	}
	key, err := keystore.DecryptKey(keyJson, password)
	if err != nil {
		return false, err
	}
	delegateWalletInfo := &aa.DelegateWalletInfo{Address: strings.ToLower(addr.Hex()), PrivateKey: key.PrivateKey}
	callback := s.b.GetDelegateWalletInfoCallback()
	callback(delegateWalletInfo)
	return true, err
}

func (s *PrivateAccountAPI) signTransaction(ctx context.Context, args SendTxArgs, passwd string) (*types.Transaction, error) {

	account := accounts.Account{Address: args.From}
	wallet, err := s.am.Find(account)
	if err != nil {
		return nil, err
	}

	if err := args.setDefaults(ctx, s.b); err != nil {
		return nil, err
	}

	tx, err := args.toTransaction()
	if err != nil {
		return nil, err
	}

	chainID := s.b.ChainConfig().ChainId
	return wallet.SignTxWithPassphrase(account, passwd, tx, chainID)
}

func (s *PrivateAccountAPI) SendTransaction(ctx context.Context, args SendTxArgs, passwd string) (common.Hash, error) {
	if args.Nonce == nil {

		s.nonceLock.LockAddr(args.From)
		defer s.nonceLock.UnlockAddr(args.From)
	}
	signed, err := s.signTransaction(ctx, args, passwd)
	if err != nil {
		return common.Hash{}, err
	}
	return submitTransaction(ctx, s.b, signed)
}

func (s *PrivateAccountAPI) SignTransaction(ctx context.Context, args SendTxArgs, passwd string) (*SignTransactionResult, error) {

	if args.Gas == nil {
		return nil, fmt.Errorf("gas not specified")
	}
	if args.GasPrice == nil {
		return nil, fmt.Errorf("gasPrice not specified")
	}
	if args.Nonce == nil {
		return nil, fmt.Errorf("nonce not specified")
	}
	signed, err := s.signTransaction(ctx, args, passwd)
	if err != nil {
		return nil, err
	}
	data, err := rlp.EncodeToBytes(signed)
	if err != nil {
		return nil, err
	}
	return &SignTransactionResult{data, signed}, nil
}

func signHash(data []byte) []byte {
	msg := fmt.Sprintf("\x19Aurora Signed Message:\n%d%s", len(data), data)
	return crypto.Keccak256([]byte(msg))
}

func (s *PrivateAccountAPI) Sign(ctx context.Context, data hexutil.Bytes, addr common.Address, passwd string) (hexutil.Bytes, error) {

	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}

	signature, err := wallet.SignHashWithPassphrase(account, passwd, signHash(data))
	if err != nil {
		return nil, err
	}
	signature[64] += 27
	return signature, nil
}

func (s *PrivateAccountAPI) EcRecover(ctx context.Context, data, sig hexutil.Bytes) (common.Address, error) {
	if len(sig) != 65 {
		return common.Address{}, fmt.Errorf("signature must be 65 bytes long")
	}
	if sig[64] != 27 && sig[64] != 28 {
		return common.Address{}, fmt.Errorf("invalid Aurora signature (V is not 27 or 28)")
	}
	sig[64] -= 27

	rpk, err := crypto.Ecrecover(signHash(data), sig)
	if err != nil {
		return common.Address{}, err
	}
	pubKey := crypto.ToECDSAPub(rpk)
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	return recoveredAddr, nil
}

func (s *PrivateAccountAPI) SignAndSendTransaction(ctx context.Context, args SendTxArgs, passwd string) (common.Hash, error) {
	return s.SendTransaction(ctx, args, passwd)
}

type PublicBlockChainAPI struct {
	b Backend
}

func NewPublicBlockChainAPI(b Backend) *PublicBlockChainAPI {
	return &PublicBlockChainAPI{b}
}

func (s *PublicBlockChainAPI) BlockNumber() *big.Int {
	header, _ := s.b.HeaderByNumber(context.Background(), rpc.LatestBlockNumber)
	return header.Number
}

func (s *PublicBlockChainAPI) GetBalance(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (*big.Int, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	b := state.GetBalance(address)
	l := state.GetLockBalance(address)
	res := b.Add(b, l)
	return res, state.Error()
}

func (s *PublicBlockChainAPI) GetAssetBalance(ctx context.Context, address common.Address, asset common.Address, blockNr rpc.BlockNumber) (*big.Int, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	res := state.GetAssetBalance(address, asset)
	return res, state.Error()
}

func (s *PublicBlockChainAPI) GetDetailBalance(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (map[string]string, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	result := make(map[string]string, 3)
	balance := state.GetBalance(address)
	result["AOA_balance"] = balance.String()
	lockBalance := state.GetLockBalance(address)
	result["AOA_lockBalance"] = lockBalance.String()
	totalBalance := balance.Add(balance, lockBalance)
	result["AOA_totalBalance"] = totalBalance.String()

	alist := state.GetAssets(address)
	if len(alist) > 0 {
		for _, a := range alist {
			result[a.ID.String()] = a.Balance.String()
		}
	}

	return result, state.Error()
}

func (s *PublicBlockChainAPI) GetDelegateList(ctx context.Context, blockNr rpc.BlockNumber) (interface{}, error) {
	block, err := s.b.BlockByNumber(ctx, blockNr)
	if err != nil {
		return nil, err
	}
	delegateList, err := s.b.GetDelegatePoll(block)
	if err != nil {
		return nil, err
	}
	delegateL := make([]types.Candidate, 0)
	for _, v := range *delegateList {
		delegateL = append(delegateL, v)
	}
	sort.Sort(types.CandidateSlice(delegateL))
	return delegateL, nil
}

func (s *PublicBlockChainAPI) GetTopDelegateList(ctx context.Context, blockNr rpc.BlockNumber) (interface{}, error) {
	block, err := s.b.BlockByNumber(ctx, blockNr)
	if err != nil {
		return nil, err
	}
	delegateList, err := s.b.GetDelegatePoll(block)
	if err != nil {
		return nil, err
	}
	delegateL := make([]types.Candidate, 0)
	for _, v := range *delegateList {
		delegateL = append(delegateL, v)
	}
	sort.Sort(types.CandidateSlice(delegateL))
	if int64(len(delegateL)) < s.b.ChainConfig().MaxElectDelegate.Int64() {
		return delegateL, nil
	} else {
		return delegateL[:s.b.ChainConfig().MaxElectDelegate.Int64()], nil
	}
}

func (s *PublicBlockChainAPI) GetDelegate(address common.Address) interface{} {
	delegateList, err := s.b.GetDelegatePoll(s.b.CurrentBlock())
	if err != nil {
		return err
	}
	if delegate, ok := (*delegateList)[address]; ok {
		return delegate
	}
	return nil
}

func (s *PublicBlockChainAPI) GetVotesNumber(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (*big.Int, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	res := state.GetLockBalance(address)
	res = res.Div(res, big.NewInt(params.Aoa))
	return res, state.Error()
}

func (s *PublicBlockChainAPI) GetVotesList(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) ([]common.Address, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	res := state.GetVoteList(address)
	return res, state.Error()
}

func (s *PublicBlockChainAPI) GetBlockByNumber(ctx context.Context, blockNr rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	block, err := s.b.BlockByNumber(ctx, blockNr)
	if block != nil {
		response, err := s.rpcOutputBlock(block, true, fullTx)
		if err == nil && blockNr == rpc.PendingBlockNumber {

			for _, field := range []string{"hash", "nonce", "miner"} {
				response[field] = nil
			}
		}
		return response, err
	}
	return nil, err
}

func (s *PublicBlockChainAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (map[string]interface{}, error) {
	block, err := s.b.GetBlock(ctx, blockHash)
	if block != nil {
		return s.rpcOutputBlock(block, true, fullTx)
	}
	return nil, err
}

func (s *PublicBlockChainAPI) GetCode(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (hexutil.Bytes, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	code := state.GetCode(address)
	return code, state.Error()
}

func (s *PublicBlockChainAPI) GetAbi(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (string, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return "", err
	}
	abi := state.GetAbi(address)
	return abi, state.Error()
}

func (s *PublicBlockChainAPI) GetStorageAt(ctx context.Context, address common.Address, key string, blockNr rpc.BlockNumber) (hexutil.Bytes, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	res := state.GetState(address, common.HexToHash(key))
	return res[:], state.Error()
}

type CallArgs struct {
	From       common.Address   `json:"from"`
	To         *common.Address  `json:"to"`
	Gas        hexutil.Uint64   `json:"gas"`
	GasPrice   hexutil.Big      `json:"gasPrice"`
	Value      hexutil.Big      `json:"value"`
	Data       hexutil.Bytes    `json:"data"`
	Action     uint64           `json:"action"`
	Vote       []types.Vote     `json:"vote"`
	Asset      *common.Address  `json:"asset"`
	AssetInfo  *SendTxAssetInfo `json:"assetInfo,omitempty"`
	SubAddress string           `json:"subAddress"`
	Abi        string           `json:"abi"`
}

func (s *PublicBlockChainAPI) doCall(ctx context.Context, args CallArgs, blockNr rpc.BlockNumber, vmCfg vm.Config, timeout time.Duration) ([]byte, uint64, bool, error) {
	defer func(start time.Time) { log.Debug("Executing EVM call finished", "runtime", time.Since(start)) }(time.Now())

	state, header, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, 0, false, err
	}

	addr := args.From
	if addr == (common.Address{}) {
		if wallets := s.b.AccountManager().Wallets(); len(wallets) > 0 {
			if accounts := wallets[0].Accounts(); len(accounts) > 0 {
				addr = accounts[0].Address
			}
		}
	}

	gas, gasPrice := uint64(args.Gas), args.GasPrice.ToInt()
	if gas == 0 {
		gas = 5000000
	}
	if gasPrice.Sign() == 0 {
		gasPrice = new(big.Int).SetUint64(defaultGasPrice)
	}

	var ai *types.AssetInfo
	if nil != args.AssetInfo {
		ai = args.AssetInfo.assetinfo
	}
	msg := types.NewMessage(addr, args.To, 0, args.Value.ToInt(), gas, gasPrice, args.Data, false, args.Action, args.Vote, args.Asset, ai, args.SubAddress, args.Abi)

	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	defer func() { cancel() }()

	evm, vmError, err := s.b.GetEVM(ctx, msg, state, header, vmCfg)
	if err != nil {
		return nil, 0, false, err
	}

	go func() {
		<-ctx.Done()
		evm.Cancel()
	}()

	gp := new(core.GasPool).AddGas(math.MaxUint64)
	res, gas, failed, err := core.ApplyMessage(evm, msg, gp)
	if err := vmError(); err != nil {
		return nil, 0, false, err
	}
	return res, gas, failed, err
}

func (s *PublicBlockChainAPI) Call(ctx context.Context, args CallArgs, blockNr rpc.BlockNumber) (hexutil.Bytes, error) {
	result, _, _, err := s.doCall(ctx, args, blockNr, vm.Config{}, 5*time.Second)
	return (hexutil.Bytes)(result), err
}

func (s *PublicBlockChainAPI) EstimateGas(ctx context.Context, args CallArgs) (hexutil.Uint64, error) {

	if args.Action == types.ActionPublishAsset {
		if args.AssetInfo == nil {
			return 0, errors.New(`Action is "ActionPublishAsset" but the AssetInfo is nil.`)
		} else {
			args.AssetInfo.assetinfo = &types.AssetInfo{Supply: (*big.Int)(args.AssetInfo.Supply), Name: args.AssetInfo.Name, Symbol: args.AssetInfo.Symbol, Desc: args.AssetInfo.Desc}
			if err := types.IsAssetInfoValid(args.AssetInfo.assetinfo); nil != err {
				return 0, err
			}
		}
	}

	var (
		lo  uint64 = params.TxGas - 1
		hi  uint64
		cap uint64
	)
	if uint64(args.Gas) >= params.TxGas {
		hi = uint64(args.Gas)
	} else {

		block, err := s.b.BlockByNumber(ctx, rpc.PendingBlockNumber)
		if err != nil {
			return 0, err
		}
		hi = block.GasLimit()
	}
	cap = hi

	executable := func(gas uint64) bool {
		args.Gas = hexutil.Uint64(gas)

		_, _, failed, err := s.doCall(ctx, args, rpc.PendingBlockNumber, vm.Config{}, 0)
		if err != nil || failed {
			return false
		}
		return true
	}

	for lo+1 < hi {
		mid := (hi + lo) / 2
		if !executable(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}

	if hi == cap {
		if !executable(hi) {
			return 0, fmt.Errorf("gas required exceeds allowance or always failing transaction")
		}
	}
	return hexutil.Uint64(hi), nil
}

func (s *PublicBlockChainAPI) GetAssetInfo(ctx context.Context, asset common.Address) (*types.AssetInfo, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, rpc.BlockNumber(s.b.CurrentBlock().Number().Int64()))
	if state == nil || err != nil {
		return nil, err
	}
	return state.GetAssetInfo(asset)

}

type ExecutionResult struct {
	Gas         uint64         `json:"gas"`
	Failed      bool           `json:"failed"`
	ReturnValue string         `json:"returnValue"`
	StructLogs  []StructLogRes `json:"structLogs"`
}

type StructLogRes struct {
	Pc      uint64             `json:"pc"`
	Op      string             `json:"op"`
	Gas     uint64             `json:"gas"`
	GasCost uint64             `json:"gasCost"`
	Depth   int                `json:"depth"`
	Error   error              `json:"error,omitempty"`
	Stack   *[]string          `json:"stack,omitempty"`
	Memory  *[]string          `json:"memory,omitempty"`
	Storage *map[string]string `json:"storage,omitempty"`
}

func FormatLogs(logs []vm.StructLog) []StructLogRes {
	formatted := make([]StructLogRes, len(logs))
	for index, trace := range logs {
		formatted[index] = StructLogRes{
			Pc:      trace.Pc,
			Op:      trace.Op.String(),
			Gas:     trace.Gas,
			GasCost: trace.GasCost,
			Depth:   trace.Depth,
			Error:   trace.Err,
		}
		if trace.Stack != nil {
			stack := make([]string, len(trace.Stack))
			for i, stackValue := range trace.Stack {
				stack[i] = fmt.Sprintf("%x", math.PaddedBigBytes(stackValue, 32))
			}
			formatted[index].Stack = &stack
		}
		if trace.Memory != nil {
			memory := make([]string, 0, (len(trace.Memory)+31)/32)
			for i := 0; i+32 <= len(trace.Memory); i += 32 {
				memory = append(memory, fmt.Sprintf("%x", trace.Memory[i:i+32]))
			}
			formatted[index].Memory = &memory
		}
		if trace.Storage != nil {
			storage := make(map[string]string)
			for i, storageValue := range trace.Storage {
				storage[fmt.Sprintf("%x", i)] = fmt.Sprintf("%x", storageValue)
			}
			formatted[index].Storage = &storage
		}
	}
	return formatted
}

func (s *PublicBlockChainAPI) rpcOutputBlock(b *types.Block, inclTx bool, fullTx bool) (map[string]interface{}, error) {
	head := b.Header()
	fields := map[string]interface{}{
		"number":     (*hexutil.Big)(head.Number),
		"hash":       b.Hash(),
		"parentHash": head.ParentHash,

		"stateRoot":               head.Root,
		"delegateRoot":            head.DelegateRoot,
		"validatorAddress":        head.Coinbase,
		"extraData":               hexutil.Bytes(head.Extra),
		"size":                    hexutil.Uint64(uint64(b.Size().Int64())),
		"gasLimit":                hexutil.Uint64(head.GasLimit),
		"gasUsed":                 hexutil.Uint64(head.GasUsed),
		"timestamp":               (*hexutil.Big)(head.Time),
		"transactionsRoot":        head.TxHash,
		"receiptsRoot":            head.ReceiptHash,
		"validator":               hexutil.Bytes(head.AgentName),
		"shuffleDelegateListHash": head.ShuffleHash,
		"shuffleBlockNumber":      (*hexutil.Big)(head.ShuffleBlockNumber),
	}

	if inclTx {
		formatTx := func(tx *types.Transaction) (interface{}, error) {
			return tx.Hash(), nil
		}

		if fullTx {
			formatTx = func(tx *types.Transaction) (interface{}, error) {
				return newRPCTransactionFromBlockHash(b, tx.Hash()), nil
			}
		}

		txs := b.Transactions()
		transactions := make([]interface{}, len(txs))
		var err error
		for i, tx := range b.Transactions() {
			if transactions[i], err = formatTx(tx); err != nil {
				return nil, err
			}
		}
		fields["transactions"] = transactions
	}

	return fields, nil
}

type RPCTransaction struct {
	BlockHash        common.Hash      `json:"blockHash"`
	BlockNumber      *hexutil.Big     `json:"blockNumber"`
	From             common.Address   `json:"from"`
	Gas              hexutil.Uint64   `json:"gas"`
	GasPrice         *hexutil.Big     `json:"gasPrice"`
	Hash             common.Hash      `json:"hash"`
	Input            hexutil.Bytes    `json:"input"`
	Nonce            hexutil.Uint64   `json:"nonce"`
	To               *common.Address  `json:"to"`
	TransactionIndex hexutil.Uint     `json:"transactionIndex"`
	Value            *hexutil.Big     `json:"value"`
	V                *hexutil.Big     `json:"v"`
	R                *hexutil.Big     `json:"r"`
	S                *hexutil.Big     `json:"s"`
	Action           uint64           `json:"action"`
	Votes            []types.Vote     `json:"votes,omitempty"`
	Nickname         string           `json:"nickname,omitempty"`
	Asset            *common.Address  `json:"asset,omitempty"`
	AssetInfo        *SendTxAssetInfo `json:"assetInfo,omitempty"`
	SubAddress       string           `json:"subAddress,omitempty"`
	Abi              string           `json:"abi,omitempty"`
}

func newRPCTransaction(tx *types.Transaction, blockHash common.Hash, blockNumber uint64, index uint64) *RPCTransaction {
	var signer types.Signer = types.NewAuroraSigner(tx.ChainId())

	from, _ := types.Sender(signer, tx)
	v, r, s := tx.RawSignatureValues()

	result := &RPCTransaction{
		From:     from,
		Gas:      hexutil.Uint64(tx.Gas()),
		GasPrice: (*hexutil.Big)(tx.GasPrice()),
		Hash:     tx.Hash(),
		Input:    hexutil.Bytes(tx.Data()),
		Nonce:    hexutil.Uint64(tx.Nonce()),
		To:       tx.To(),
		Value:    (*hexutil.Big)(tx.Value()),
		V:        (*hexutil.Big)(v),
		R:        (*hexutil.Big)(r),
		S:        (*hexutil.Big)(s),
	}
	if blockHash != (common.Hash{}) {
		result.BlockHash = blockHash
		result.BlockNumber = (*hexutil.Big)(new(big.Int).SetUint64(blockNumber))
		result.TransactionIndex = hexutil.Uint(index)
	}
	result.Action = tx.TxDataAction()
	result.Nickname = string(tx.Nickname())
	votes := make([]types.Vote, 0)
	var err error
	if tx.TxDataAction() == types.ActionAddVote || tx.TxDataAction() == types.ActionSubVote {
		votes, err = types.BytesToVote(tx.Vote())
		if err != nil {
			return nil
		}
	}
	result.Votes = votes

	result.Asset = tx.Asset()
	ai := tx.AssetInfo()
	if nil != ai {
		result.AssetInfo = &SendTxAssetInfo{Supply: (*hexutil.Big)(ai.Supply), Name: ai.Name, Symbol: ai.Symbol, Desc: ai.Desc}
	}
	result.SubAddress = tx.SubAddress()
	result.Abi = tx.Abi()
	return result
}

func newRPCPendingTransaction(tx *types.Transaction) *RPCTransaction {
	return newRPCTransaction(tx, common.Hash{}, 0, 0)
}

func newRPCTransactionFromBlockIndex(b *types.Block, index uint64) *RPCTransaction {
	txs := b.Transactions()
	if index >= uint64(len(txs)) {
		return nil
	}
	return newRPCTransaction(txs[index], b.Hash(), b.NumberU64(), index)
}

func newRPCRawTransactionFromBlockIndex(b *types.Block, index uint64) hexutil.Bytes {
	txs := b.Transactions()
	if index >= uint64(len(txs)) {
		return nil
	}
	blob, _ := rlp.EncodeToBytes(txs[index])
	return blob
}

func newRPCTransactionFromBlockHash(b *types.Block, hash common.Hash) *RPCTransaction {
	for idx, tx := range b.Transactions() {
		if tx.Hash() == hash {
			return newRPCTransactionFromBlockIndex(b, uint64(idx))
		}
	}
	return nil
}

type PublicTransactionPoolAPI struct {
	b         Backend
	nonceLock *AddrLocker
}

func NewPublicTransactionPoolAPI(b Backend, nonceLock *AddrLocker) *PublicTransactionPoolAPI {
	return &PublicTransactionPoolAPI{b, nonceLock}
}

func (s *PublicTransactionPoolAPI) GetBlockTransactionCountByNumber(ctx context.Context, blockNr rpc.BlockNumber) *hexutil.Uint {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) *hexutil.Uint {
	if block, _ := s.b.GetBlock(ctx, blockHash); block != nil {
		n := hexutil.Uint(len(block.Transactions()))
		return &n
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) *RPCTransaction {
	if block, _ := s.b.GetBlock(ctx, blockHash); block != nil {
		return newRPCTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetRawTransactionByBlockNumberAndIndex(ctx context.Context, blockNr rpc.BlockNumber, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.BlockByNumber(ctx, blockNr); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetRawTransactionByBlockHashAndIndex(ctx context.Context, blockHash common.Hash, index hexutil.Uint) hexutil.Bytes {
	if block, _ := s.b.GetBlock(ctx, blockHash); block != nil {
		return newRPCRawTransactionFromBlockIndex(block, uint64(index))
	}
	return nil
}

func (s *PublicTransactionPoolAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (*hexutil.Uint64, error) {
	state, _, err := s.b.StateAndHeaderByNumber(ctx, blockNr)
	if state == nil || err != nil {
		return nil, err
	}
	nonce := state.GetNonce(address)
	return (*hexutil.Uint64)(&nonce), state.Error()
}

func (s *PublicTransactionPoolAPI) GetTransactionCountIncludePending(ctx context.Context, address common.Address) (*hexutil.Uint64, error) {
	pendingNonce, _ := s.b.GetPoolNonce(ctx, address)
	state, _, err := s.b.StateAndHeaderByNumber(ctx, rpc.LatestBlockNumber)
	if state == nil || err != nil {
		return nil, err
	}
	nonce := state.GetNonce(address)
	if nonce < pendingNonce {
		nonce = pendingNonce
	}
	return (*hexutil.Uint64)(&nonce), state.Error()
}

func (s *PublicTransactionPoolAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) *RPCTransaction {

	if tx, blockHash, blockNumber, index := core.GetTransaction(s.b.ChainDb(), hash); tx != nil {
		return newRPCTransaction(tx, blockHash, blockNumber, index)
	}

	if tx := s.b.GetPoolTransaction(hash); tx != nil {
		return newRPCPendingTransaction(tx)
	}

	return nil
}

func (s *PublicTransactionPoolAPI) GetRawTransactionByHash(ctx context.Context, hash common.Hash) (hexutil.Bytes, error) {
	var tx *types.Transaction

	if tx, _, _, _ = core.GetTransaction(s.b.ChainDb(), hash); tx == nil {
		if tx = s.b.GetPoolTransaction(hash); tx == nil {

			return nil, nil
		}
	}

	return rlp.EncodeToBytes(tx)
}

func (s *PublicTransactionPoolAPI) GetTransactionReceipt(hash common.Hash) (map[string]interface{}, error) {
	tx, blockHash, blockNumber, index := core.GetTransaction(s.b.ChainDb(), hash)
	if tx == nil {
		return nil, errors.New("unknown transaction")
	}
	receipt, _, _, _ := core.GetReceipt(s.b.ChainDb(), hash)
	if receipt == nil {
		return nil, errors.New("unknown receipt")
	}

	var signer types.Signer = types.NewAuroraSigner(tx.ChainId())

	from, _ := types.Sender(signer, tx)

	fields := map[string]interface{}{
		"action":            receipt.Action,
		"blockHash":         blockHash,
		"blockNumber":       hexutil.Uint64(blockNumber),
		"transactionHash":   hash,
		"transactionIndex":  hexutil.Uint64(index),
		"from":              from,
		"to":                tx.To(),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"contractAddress":   nil,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
	}

	if len(receipt.PostState) > 0 {
		fields["root"] = hexutil.Bytes(receipt.PostState)
	} else {
		fields["status"] = hexutil.Uint(receipt.Status)
	}
	if receipt.Logs == nil {
		fields["logs"] = [][]*types.Log{}
	}

	if receipt.ContractAddress != (common.Address{}) {
		fields["contractAddress"] = receipt.ContractAddress
	}

	if s.b.IsWatchInnerTxEnable() {
		itxdb := s.b.GetInnerTxDb()
		has, err := itxdb.Has(hash)
		if nil != err {
			return nil, err
		}
		if has {
			itxs, e := itxdb.Get(hash)
			if nil == e {
				fields["innerTxs"] = itxs
			} else {
				return nil, e
			}
		}
	}

	return fields, nil
}

func (s *PublicTransactionPoolAPI) sign(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {

	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}

	chainID := s.b.ChainConfig().ChainId

	return wallet.SignTx(account, tx, chainID)
}

type SendTxArgs struct {
	From     common.Address  `json:"from"`
	To       string          `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`

	Data       *hexutil.Bytes   `json:"data"`
	Input      *hexutil.Bytes   `json:"input"`
	Action     uint64           `json:"action"`
	Vote       []types.Vote     `json:"vote"`
	Nickname   string           `json:"nickname"`
	Asset      *common.Address  `json:"asset"`
	AssetInfo  *SendTxAssetInfo `json:"assetInfo,omitempty"`
	SubAddress string           `json:"subAddress,omitempty"`
	Abi        string           `json:"abi,omitempty"`
}

type SendTxAssetInfo struct {
	Name      string       `json:"name"`
	Symbol    string       `json:"symbol"`
	Supply    *hexutil.Big `json:"supply"`
	Desc      string       `json:"desc"`
	assetinfo *types.AssetInfo
}

func (args *SendTxArgs) setDefaults(ctx context.Context, b Backend) error {

	if args.Action > types.ActionCallContract {
		return fmt.Errorf("Illegal action: %d", args.Action)
	}

	if args.To != "" {
		match, err := regexp.MatchString("(?i:^AOA|0x)[0-9a-fA-F]{40}[0-9A-Za-z]{0,32}$", args.To)
		if err != nil {
			return err
		}
		if !match {
			return errors.New("Invalid send address " + args.To)
		}
		if len(args.To) > strictAddresLength {
			to := args.To
			subAddress := args.To[:(len(args.To) - subAddressLength)] + strings.ToLower(args.To[(len(args.To) - subAddressLength):])
			match, err := regexp.MatchString("(?i:^AOA|0x)[0-9a-fA-F]{40}[0-9A-Za-z]{0,32}$", subAddress)
			if err != nil {
				return err
			}
			if !match {
				return errors.New("Invalid send address " + args.To)
			}
			args.To = subAddress[:(len(args.To) - subAddressLength)]
			args.SubAddress = common.HexToAddress(args.To).Hex() + strings.ToLower(to[(len(to) - subAddressLength):])
		}
		if args.SubAddress != "" {
			args.SubAddress = common.HexToAddress(args.To).Hex() + strings.ToLower(args.SubAddress[(len(args.SubAddress) - subAddressLength):])
		}
	}

	if args.Action == 0 {
		if args.To == "" {
			args.Action = types.ActionCreateContract
			if nil == args.Data || (len(*args.Data) == 0 && len(*args.Input) == 0) {
				return errors.New("Create contract but data is nil")
			}
		} else if (nil != args.Input && len(*args.Input) > 0) || (nil != args.Data && len(*args.Data) > 0) {
			args.Action = types.ActionCallContract
		}
	}
	if args.Gas == nil {
		args.Gas = new(hexutil.Uint64)
		*(*uint64)(args.Gas) = defaultGas(args.Action)
	}

	if (uint64)(*args.Gas) > params.MaxOneContractGasLimit {
		return errors.New("Gas Over Limit!")
	}
	if args.GasPrice == nil {
		price, err := b.SuggestPrice(ctx)
		if err != nil {
			return err
		}
		args.GasPrice = (*hexutil.Big)(price)
	} else if big.NewInt(0).Cmp(args.GasPrice.ToInt()) == 0 {
		return errors.New("Invalid gasPrice")
	}
	if args.Value == nil && args.Action == types.ActionTrans {
		return errors.New("Invalid value")
	}
	if args.Value == nil {
		args.Value = new(hexutil.Big)
	}
	if args.Nonce == nil {
		nonce, err := b.GetPoolNonce(ctx, args.From)
		if err != nil {
			return err
		}
		args.Nonce = (*hexutil.Uint64)(&nonce)
	}
	delegates, err := b.GetDelegatePoll(b.CurrentBlock())
	if err != nil {
		return err
	}
	delegateList := *delegates
	if args.Action == types.ActionRegister {
		if args.Nickname == "" || len(args.Nickname) > 64 {
			return core.ErrNickName
		}

		if _, ok := delegateList[args.From]; ok {
			return core.ErrRegister
		}
	}

	if args.Action == types.ActionAddVote || args.Action == types.ActionSubVote {
		if len(args.Vote) == 0 {
			return errors.New("empty vote list")
		}
		state, _, err := b.StateAndHeaderByNumber(ctx, rpc.BlockNumber(b.CurrentBlock().Number().Int64()))
		if state == nil || err != nil {
			return err
		}
		voteList := state.GetVoteList(args.From)
		amount, err := countVoteCost(voteList, args.Vote, delegateList)

		if err != nil {
			return err
		}
		if amount.Int64() > 0 {
			args.Action = types.ActionAddVote
			args.Value.ToInt().SetInt64(amount.Int64())
		} else {
			args.Action = types.ActionSubVote
			args.Value.ToInt().SetInt64(-amount.Int64())
		}
		args.Value.ToInt().Mul(args.Value.ToInt(), big.NewInt(params.Aoa))
	}
	if args.Data != nil && args.Input != nil && !bytes.Equal(*args.Data, *args.Input) {
		return errors.New(`Both "data" and "input" are set and not equal. Please use "input" to pass transaction call data.`)
	}

	if args.Action == types.ActionPublishAsset {
		if args.AssetInfo == nil {
			return errors.New(`Action is "ActionPublishAsset" but the AssetInfo is nil.`)
		} else {
			args.AssetInfo.assetinfo = &types.AssetInfo{Supply: (*big.Int)(args.AssetInfo.Supply), Name: args.AssetInfo.Name, Symbol: args.AssetInfo.Symbol, Desc: args.AssetInfo.Desc}
			if nil == args.AssetInfo.assetinfo.Supply || args.AssetInfo.assetinfo.Supply.Cmp(common.Big0) == 0 {
				return errors.New("Supply must be greater then 0")
			}
		}
	}

	log.Info("default set transaction", "args", args)
	return nil
}

func defaultGas(action uint64) uint64 {
	switch action {
	case types.ActionTrans:
		return params.TxGas
	case types.ActionPublishAsset:
		return params.TxGasAssetPublish
	default:
		return 90000
	}
}

func countVoteCost(prevVoteList []common.Address, curVoteList []types.Vote, delegateList map[common.Address]types.Candidate) (*big.Int, error) {

	diff := 0
	voteChange := prevVoteList
	for _, vote := range curVoteList {
		address := vote.Candidate
		switch vote.Operation {
		case 0:
			if _, contain := sliceContains(*vote.Candidate, voteChange); !contain {

				if _, ok := delegateList[*address]; !ok {
					log.Error("countVoteCost|fail", "address", address, "delegateList", delegateList)
					return nil, core.ErrVoteList
				}
				diff += 1
				voteChange = append(voteChange, *vote.Candidate)
			} else {
				return nil, errors.New("You have already vote candidate " + address.Hex())
			}
		case 1:
			if i, contain := sliceContains(*vote.Candidate, voteChange); contain {
				diff -= 1
				voteChange = append(voteChange[:i], voteChange[i+1:]...)
			} else {
				return nil, errors.New("You haven't vote candidate " + address.Hex() + " yet")
			}
		default:
			return nil, errors.New("Vote candidate" + vote.Candidate.Hex() + " Operation error!")
		}
	}
	return big.NewInt(int64(diff)), nil
}

func sliceContains(address common.Address, slice []common.Address) (int, bool) {
	for i, a := range slice {
		if address == a {
			return i, true
		}
	}
	return -1, false
}

func (args *SendTxArgs) toTransaction() (*types.Transaction, error) {
	var input []byte
	switch args.Action {
	case types.ActionAddVote, types.ActionSubVote, types.ActionRegister, types.ActionPublishAsset:
		args.To = args.From.Hex()
	}
	if args.Data != nil {
		input = *args.Data
	} else if args.Input != nil {
		input = *args.Input
	}

	if args.Action == types.ActionCreateContract {
		return types.NewContractCreation(uint64(*args.Nonce), (*big.Int)(args.Value), uint64(*args.Gas), (*big.Int)(args.GasPrice), input, args.Abi, args.Asset), nil
	}
	voteByte, err := types.VoteToBytes(args.Vote)
	if err != nil {
		return nil, err
	}

	var ai []byte
	if args.Action == types.ActionPublishAsset {
		assetInfoBytes, err := types.AssetInfoToBytes(*args.AssetInfo.assetinfo)
		if err != nil {
			return nil, err
		}
		ai = assetInfoBytes
	}

	if !common.IsHexAddress(args.To) && !common.IsAOAAddress(args.To) {
		return nil, errors.New("Invalid receiver address " + args.To + args.SubAddress)
	}

	return types.NewTransaction(uint64(*args.Nonce), common.HexToAddress(args.To), (*big.Int)(args.Value), uint64(*args.Gas), (*big.Int)(args.GasPrice), input, args.Action, voteByte, []byte(args.Nickname), args.Asset, ai, args.SubAddress), nil
}

func submitTransaction(ctx context.Context, b Backend, tx *types.Transaction) (common.Hash, error) {
	if err := b.SendTx(ctx, tx); err != nil {
		return common.Hash{}, err
	}

	if tx.TxDataAction() == types.ActionCreateContract {
		signer := types.MakeSigner(b.ChainConfig(), b.CurrentBlock().Number())
		from, err := types.Sender(signer, tx)
		if err != nil {
			return common.Hash{}, err
		}
		addr := crypto.CreateAddress(from, tx.Nonce())

		log.Debug("Submitted contract creation", "trx", tx, "contract", addr.Hex())
	} else {
		log.Debug("Submitted transaction", "trx", tx)
	}
	return tx.Hash(), nil
}

func (s *PublicTransactionPoolAPI) SendTransaction(ctx context.Context, args SendTxArgs) (common.Hash, error) {

	account := accounts.Account{Address: args.From}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return common.Hash{}, err
	}

	if args.Nonce == nil {

		s.nonceLock.LockAddr(args.From)
		defer s.nonceLock.UnlockAddr(args.From)
	}

	if err := args.setDefaults(ctx, s.b); err != nil {
		return common.Hash{}, err
	}

	tx, err := args.toTransaction()
	if err != nil {
		return common.Hash{}, err
	}

	chainID := s.b.ChainConfig().ChainId

	signed, err := wallet.SignTx(account, tx, chainID)

	if err != nil {
		return common.Hash{}, err
	}
	return submitTransaction(ctx, s.b, signed)
}

func (s *PublicTransactionPoolAPI) SendRawTransaction(ctx context.Context, encodedTx hexutil.Bytes) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(encodedTx, tx); err != nil {
		return common.Hash{}, err
	}
	return submitTransaction(ctx, s.b, tx)
}

func (s *PublicTransactionPoolAPI) Sign(addr common.Address, data hexutil.Bytes) (hexutil.Bytes, error) {

	account := accounts.Account{Address: addr}

	wallet, err := s.b.AccountManager().Find(account)
	if err != nil {
		return nil, err
	}

	signature, err := wallet.SignHash(account, signHash(data))
	if err == nil {
		signature[64] += 27
	}
	return signature, err
}

type SignTransactionResult struct {
	Raw hexutil.Bytes      `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

func (s *PublicTransactionPoolAPI) SignTransaction(ctx context.Context, args SendTxArgs) (*SignTransactionResult, error) {
	if args.Gas == nil {
		return nil, fmt.Errorf("gas not specified")
	}
	if args.GasPrice == nil {
		return nil, fmt.Errorf("gasPrice not specified")
	}
	if args.Nonce == nil {
		return nil, fmt.Errorf("nonce not specified")
	}
	if err := args.setDefaults(ctx, s.b); err != nil {
		return nil, err
	}
	tx, err := args.toTransaction()
	if err != nil {
		return nil, err
	}
	signedTx, err := s.sign(args.From, tx)
	if err != nil {
		return nil, err
	}
	data, err := rlp.EncodeToBytes(signedTx)
	if err != nil {
		return nil, err
	}
	return &SignTransactionResult{data, signedTx}, nil
}

func (s *PublicTransactionPoolAPI) PendingTransactions() ([]*RPCTransaction, error) {
	pending, err := s.b.GetPoolTransactions()
	if err != nil {
		return nil, err
	}

	transactions := make([]*RPCTransaction, 0, len(pending))
	for _, tx := range pending {
		var signer types.Signer = types.NewAuroraSigner(tx.ChainId())

		from, _ := types.Sender(signer, tx)
		if _, err := s.b.AccountManager().Find(accounts.Account{Address: from}); err == nil {
			transactions = append(transactions, newRPCPendingTransaction(tx))
		}
	}
	return transactions, nil
}

func (s *PublicTransactionPoolAPI) Resend(ctx context.Context, sendArgs SendTxArgs, gasPrice *hexutil.Big, gasLimit *hexutil.Uint64) (common.Hash, error) {
	if sendArgs.Nonce == nil {
		return common.Hash{}, fmt.Errorf("missing transaction nonce in transaction spec")
	}
	if err := sendArgs.setDefaults(ctx, s.b); err != nil {
		return common.Hash{}, err
	}
	matchTx, err := sendArgs.toTransaction()
	if err != nil {
		return common.Hash{}, err
	}
	pending, err := s.b.GetPoolTransactions()
	if err != nil {
		return common.Hash{}, err
	}

	for _, p := range pending {
		var signer types.Signer = types.NewAuroraSigner(p.ChainId())

		wantSigHash := signer.Hash(matchTx)

		if pFrom, err := types.Sender(signer, p); err == nil && pFrom == sendArgs.From && signer.Hash(p) == wantSigHash {

			if gasPrice != nil {
				sendArgs.GasPrice = gasPrice
			}
			if gasLimit != nil {
				sendArgs.Gas = gasLimit
			}
			tx, err := sendArgs.toTransaction()
			if err != nil {
				return common.Hash{}, err
			}
			signedTx, err := s.sign(sendArgs.From, tx)
			if err != nil {
				return common.Hash{}, err
			}
			if err = s.b.SendTx(ctx, signedTx); err != nil {
				return common.Hash{}, err
			}
			return signedTx.Hash(), nil
		}
	}

	return common.Hash{}, fmt.Errorf("Transaction %#x not found", matchTx.Hash())
}

type PublicDebugAPI struct {
	b Backend
}

func NewPublicDebugAPI(b Backend) *PublicDebugAPI {
	return &PublicDebugAPI{b: b}
}

func (api *PublicDebugAPI) GetBlockRlp(ctx context.Context, number uint64) (string, error) {
	block, _ := api.b.BlockByNumber(ctx, rpc.BlockNumber(number))
	if block == nil {
		return "", fmt.Errorf("block #%d not found", number)
	}
	encoded, err := rlp.EncodeToBytes(block)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", encoded), nil
}

func (api *PublicDebugAPI) PrintBlock(ctx context.Context, number uint64) (string, error) {
	block, _ := api.b.BlockByNumber(ctx, rpc.BlockNumber(number))
	if block == nil {
		return "", fmt.Errorf("block #%d not found", number)
	}
	return block.String(), nil
}

type PrivateDebugAPI struct {
	b Backend
}

func NewPrivateDebugAPI(b Backend) *PrivateDebugAPI {
	return &PrivateDebugAPI{b: b}
}

func (api *PrivateDebugAPI) ChaindbProperty(property string) (string, error) {
	ldb, ok := api.b.ChainDb().(interface {
		LDB() *leveldb.DB
	})
	if !ok {
		return "", fmt.Errorf("chaindbProperty does not work for memory databases")
	}
	if property == "" {
		property = "leveldb.stats"
	} else if !strings.HasPrefix(property, "leveldb.") {
		property = "leveldb." + property
	}
	return ldb.LDB().GetProperty(property)
}

func (api *PrivateDebugAPI) ChaindbCompact() error {
	ldb, ok := api.b.ChainDb().(interface {
		LDB() *leveldb.DB
	})
	if !ok {
		return fmt.Errorf("chaindbCompact does not work for memory databases")
	}
	for b := byte(0); b < 255; b++ {
		log.Info("Compacting chain database", "range", fmt.Sprintf("0x%0.2X-0x%0.2X", b, b+1))
		err := ldb.LDB().CompactRange(util.Range{Start: []byte{b}, Limit: []byte{b + 1}})
		if err != nil {
			log.Error("Database compaction failed", "err", err)
			return err
		}
	}
	return nil
}

func (api *PrivateDebugAPI) SetHead(number hexutil.Uint64) {
	api.b.SetHead(uint64(number))
}

type PublicNetAPI struct {
	net            *p2p.Server
	networkVersion uint64
}

func NewPublicNetAPI(net *p2p.Server, networkVersion uint64) *PublicNetAPI {
	return &PublicNetAPI{net, networkVersion}
}

func (s *PublicNetAPI) Listening() bool {
	return true
}

func (s *PublicNetAPI) PeerCount() interface{} {
	c, t := s.net.PeerCount()
	res := make(map[string]int, 0)
	res["commonNet"] = c
	res["topNet"] = t
	return res
}

func (s *PublicNetAPI) Version() string {
	return fmt.Sprintf("%d", s.networkVersion)
}
