package params

var (
	TargetGasLimit = GenesisGasLimit 
)

const (
	GasLimitBoundDivisor   uint64 = 512                         
	MinGasLimit            uint64 = 200000000                   
	GenesisGasLimit        uint64 = 250000000                   
	MaxGasLimit            uint64 = 300000000                   
	MaximumExtraDataSize   uint64 = 16                          
	ExpByteGas             uint64 = 1                           
	SloadGas               uint64 = 3                           
	CallValueTransferGas   uint64 = 550                         
	CallNewAccountGas      uint64 = 1600                        
	TxGas                  uint64 = 25000                       
	TxGasContractCreation  uint64 = 20000                       
	TxGasAgentCreation            = "5000000000000000000000000" 
	TxDataZeroGas          uint64 = 1                           
	QuadCoeffDiv           uint64 = 1024                        
	SstoreSetGas           uint64 = 1250                        
	LogDataGas             uint64 = 1                           
	CallStipend            uint64 = 1000                        
	TxGasAssetPublish      uint64 = 100000                      
	MaxContractGasLimit    uint64 = 60000000                     
	MaxOneContractGasLimit uint64 = 1000000                      

	BalanceOfGas     uint64 = 50
	TransferAssetGas uint64 = 550

	Sha3Gas          uint64 = 2    
	Sha3WordGas      uint64 = 1    
	SstoreResetGas   uint64 = 310  
	SstoreClearGas   uint64 = 310  
	SstoreRefundGas  uint64 = 950  
	JumpdestGas      uint64 = 1    
	EpochDuration    uint64 = 2000 
	CallGas          uint64 = 3    
	CreateDataGas    uint64 = 12   
	CallCreateDepth  uint64 = 1024 
	ExpGas           uint64 = 2    
	LogGas           uint64 = 24   
	CopyGas          uint64 = 1    
	StackLimit       uint64 = 1024 
	TierStepGas      uint64 = 0    
	LogTopicGas      uint64 = 24   
	CreateGas        uint64 = 2000 
	SuicideRefundGas uint64 = 1500 
	MemoryGas        uint64 = 1    
	TxDataNonZeroGas uint64 = 4    
	TxABIGas         uint64 = 3    

	MaxCodeSize = 24576 

	EcrecoverGas            uint64 = 200  
	Sha256BaseGas           uint64 = 4    
	Sha256PerWordGas        uint64 = 1    
	Ripemd160BaseGas        uint64 = 38   
	Ripemd160PerWordGas     uint64 = 8    
	IdentityBaseGas         uint64 = 1    
	IdentityPerWordGas      uint64 = 1    
	ModExpQuadCoeffDiv      uint64 = 2    
	Bn256AddGas             uint64 = 32   
	Bn256ScalarMulGas       uint64 = 2500 
	Bn256PairingBaseGas     uint64 = 6250 
	Bn256PairingPerPointGas uint64 = 5000 
)
