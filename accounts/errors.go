package accounts

import (
	"errors"
	"fmt"
)

var ErrUnknownAccount = errors.New("unknown account")

var ErrUnknownWallet = errors.New("unknown wallet")

var ErrNotSupported = errors.New("not supported")

var ErrInvalidPassphrase = errors.New("invalid passphrase")

var ErrWalletAlreadyOpen = errors.New("wallet already open")

var ErrWalletClosed = errors.New("wallet closed")

type AuthNeededError struct {
	Needed string
}

func NewAuthNeededError(needed string) error {
	return &AuthNeededError{
		Needed: needed,
	}
}

func (err *AuthNeededError) Error() string {
	return fmt.Sprintf("authentication needed: %s", err.Needed)
}
