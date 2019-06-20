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

package params

var (
	TargetGasLimit = GenesisGasLimit // The artificial target
)

const (
	GasLimitBoundDivisor   uint64 = 512                         // The bound divisor of the gas limit, used in update calculations.
	MinGasLimit            uint64 = 200000000                   // Minimum the gas limit may ever be.
	GenesisGasLimit        uint64 = 250000000                   // Gas limit of the Genesis block. In AOA, it's set to 250 million, meaning that a block can contain about 10 thousand transfer transactions.
	MaxGasLimit            uint64 = 300000000                   // Max block GasLimit
	MaximumExtraDataSize   uint64 = 16                          // Maximum size extra data may be after Genesis.
	ExpByteGas             uint64 = 1                           // Times ceil(log256(exponent)) for the EXP instruction.
	SloadGas               uint64 = 3                           // Multiplied by the number of 32-byte words that are copied (round up) for any *COPY operation and added.
	CallValueTransferGas   uint64 = 550                         // Paid for CALL when the value transfer is non-zero.
	CallNewAccountGas      uint64 = 1600                        // Paid for CALL when the destination address didn't exist prior.
	TxGas                  uint64 = 25000                       // Per transaction not creating a contract. NOTE: Not payable on data of calls between transactions.
	TxGasContractCreation  uint64 = 20000                       // Per transaction that creates a contract. NOTE: Not payable on data of calls between transactions.
	TxGasAgentCreation            = "5000000000000000000000000" // 注册代理花费
	TxDataZeroGas          uint64 = 1                           // Per byte of data attached to a transaction that equals zero. NOTE: Not payable on data of calls between transactions.
	QuadCoeffDiv           uint64 = 1024                        // Divisor for the quadratic particle of the memory cost equation.
	SstoreSetGas           uint64 = 1250                        // Once per SLOAD operation.
	LogDataGas             uint64 = 1                           // Per byte in a LOG* operation's data.
	CallStipend            uint64 = 1000                        // Free gas given at beginning of call.
	TxGasAssetPublish      uint64 = 100000                      // Gas for publishing an asset.
	MaxContractGasLimit    uint64 = 60000000                     //一个块合约最大GasLimit
	MaxOneContractGasLimit uint64 = 1000000                      //一笔合约交易最大GasLimit

	// Multi-asset
	BalanceOfGas     uint64 = 50
	TransferAssetGas uint64 = 550

	Sha3Gas          uint64 = 2    // Once per SHA3 operation.
	Sha3WordGas      uint64 = 1    // Once per word of the SHA3 operation's data.
	SstoreResetGas   uint64 = 310  // Once per SSTORE operation if the zeroness changes from zero.
	SstoreClearGas   uint64 = 310  // Once per SSTORE operation if the zeroness doesn't change.
	SstoreRefundGas  uint64 = 950  // Once per SSTORE operation if the zeroness changes to zero.
	JumpdestGas      uint64 = 1    // Refunded gas, once per SSTORE operation if the zeroness changes to zero.
	EpochDuration    uint64 = 2000 // Duration between proof-of-work epochs.
	CallGas          uint64 = 3    // Once per CALL operation & message call transaction.
	CreateDataGas    uint64 = 12   //
	CallCreateDepth  uint64 = 1024 // Maximum depth of call/create stack.
	ExpGas           uint64 = 2    // Once per EXP instruction
	LogGas           uint64 = 24   // Per LOG* operation.
	CopyGas          uint64 = 1    //
	StackLimit       uint64 = 1024 // Maximum size of VM stack allowed.
	TierStepGas      uint64 = 0    // Once per operation, for a selection of them.
	LogTopicGas      uint64 = 24   // Multiplied by the * of the LOG*, per LOG transaction. e.g. LOG0 incurs 0 * c_txLogTopicGas, LOG4 incurs 4 * c_txLogTopicGas.
	CreateGas        uint64 = 2000 // Once per CREATE operation & contract-creation transaction.
	SuicideRefundGas uint64 = 1500 // Refunded following a suicide operation.
	MemoryGas        uint64 = 1    // Times the address of the (highest referenced byte in memory + 1). NOTE: referencing happens on read, write and in instructions such as RETURN and CALL.
	TxDataNonZeroGas uint64 = 4    // Per byte of data attached to a transaction that is not equal to zero. NOTE: Not payable on data of calls between transactions.
	TxABIGas         uint64 = 3    // Per byte of abi attached to a transaction.

	MaxCodeSize = 24576 // Maximum bytecode to permit for a contract

	// Precompiled contract gas prices

	EcrecoverGas            uint64 = 200  // Elliptic curve sender recovery gas price
	Sha256BaseGas           uint64 = 4    // Base price for a SHA256 operation
	Sha256PerWordGas        uint64 = 1    // Per-word price for a SHA256 operation
	Ripemd160BaseGas        uint64 = 38   // Base price for a RIPEMD160 operation
	Ripemd160PerWordGas     uint64 = 8    // Per-word price for a RIPEMD160 operation
	IdentityBaseGas         uint64 = 1    // Base price for a data copy operation
	IdentityPerWordGas      uint64 = 1    // Per-work price for a data copy operation
	ModExpQuadCoeffDiv      uint64 = 2    // Divisor for the quadratic particle of the big int modular exponentiation
	Bn256AddGas             uint64 = 32   // Gas needed for an elliptic curve addition
	Bn256ScalarMulGas       uint64 = 2500 // Gas needed for an elliptic curve scalar multiplication
	Bn256PairingBaseGas     uint64 = 6250 // Base price for an elliptic curve pairing check
	Bn256PairingPerPointGas uint64 = 5000 // Per-point price for an elliptic curve pairing check
)
