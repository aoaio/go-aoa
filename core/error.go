package core

import "errors"

var (

	ErrKnownBlock = errors.New("block already known")

	ErrGasLimitReached = errors.New("gas limit reached")

	ErrBlacklistedHash = errors.New("blacklisted hash")

	ErrNonceTooHigh = errors.New("nonce too high")

	ErrDuplicateRegisterAgent = errors.New("duplicate register vote address")

	ErrAddVote = errors.New("delegate not exist when add vote")

	ErrSubVote = errors.New("delegate not exist when sub vote")

	ErrSubVoteNotEnough = errors.New("delegate sub error,vote not enough")

	ErrCancelAgent = errors.New("delegate not exist when cancel")

)
