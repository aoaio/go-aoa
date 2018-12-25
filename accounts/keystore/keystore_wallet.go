package keystore

import (
	Aurorachain "github.com/Aurorachain/go-Aurora"
	"github.com/Aurorachain/go-Aurora/accounts"
	"github.com/Aurorachain/go-Aurora/core/types"
	"math/big"
)

type keystoreWallet struct {
	account  accounts.Account
	keystore *KeyStore
}

func (w *keystoreWallet) URL() accounts.URL {
	return w.account.URL
}

func (w *keystoreWallet) Status() (string, error) {
	w.keystore.mu.RLock()
	defer w.keystore.mu.RUnlock()

	if _, ok := w.keystore.unlocked[w.account.Address]; ok {
		return "Unlocked", nil
	}
	return "Locked", nil
}

func (w *keystoreWallet) Open(passphrase string) error { return nil }

func (w *keystoreWallet) Close() error { return nil }

func (w *keystoreWallet) Accounts() []accounts.Account {
	return []accounts.Account{w.account}
}

func (w *keystoreWallet) Contains(account accounts.Account) bool {
	return account.Address == w.account.Address && (account.URL == (accounts.URL{}) || account.URL == w.account.URL)
}

func (w *keystoreWallet) Derive(path accounts.DerivationPath, pin bool) (accounts.Account, error) {
	return accounts.Account{}, accounts.ErrNotSupported
}

func (w *keystoreWallet) SelfDerive(base accounts.DerivationPath, chain Aurorachain.ChainStateReader) {}

func (w *keystoreWallet) SignHash(account accounts.Account, hash []byte) ([]byte, error) {

	if account.Address != w.account.Address {
		return nil, accounts.ErrUnknownAccount
	}
	if account.URL != (accounts.URL{}) && account.URL != w.account.URL {
		return nil, accounts.ErrUnknownAccount
	}

	return w.keystore.SignHash(account, hash)
}

func (w *keystoreWallet) SignTx(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {

	if account.Address != w.account.Address {
		return nil, accounts.ErrUnknownAccount
	}
	if account.URL != (accounts.URL{}) && account.URL != w.account.URL {
		return nil, accounts.ErrUnknownAccount
	}

	return w.keystore.SignTx(account, tx, chainID)
}

func (w *keystoreWallet) SignHashWithPassphrase(account accounts.Account, passphrase string, hash []byte) ([]byte, error) {

	if account.Address != w.account.Address {
		return nil, accounts.ErrUnknownAccount
	}
	if account.URL != (accounts.URL{}) && account.URL != w.account.URL {
		return nil, accounts.ErrUnknownAccount
	}

	return w.keystore.SignHashWithPassphrase(account, passphrase, hash)
}

func (w *keystoreWallet) SignTxWithPassphrase(account accounts.Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {

	if account.Address != w.account.Address {
		return nil, accounts.ErrUnknownAccount
	}
	if account.URL != (accounts.URL{}) && account.URL != w.account.URL {
		return nil, accounts.ErrUnknownAccount
	}

	return w.keystore.SignTxWithPassphrase(account, passphrase, tx, chainID)
}
