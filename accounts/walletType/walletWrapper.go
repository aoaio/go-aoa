package walletType

import (
	"github.com/Aurorachain/go-Aurora/accounts"
	"crypto/ecdsa"
)

type WalletWrapper struct {
	Wallet accounts.Wallet
	Pwd string
}

type DelegateWalletInfo struct {
	Address string
	PrivateKey *ecdsa.PrivateKey
}