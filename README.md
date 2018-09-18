# go-aoa
Deploy
Under Linux or Mac, get the compressed file from release and extract it to get the executable file aoa.Create the storage directory /data/aoa, and copy the executable file to the directory. Then execute the startup command, in which '--port' is the customized chain synchronization port, '--rpc' would open RPC service, '--rpcaddr' is the self-defined RPC listening IP which should set to 127.0.0.1 if you do not want the remote RPC connection, --rpcport is the customized RCP listening port.
for example
```
tar zxvf aoa-linux-amd64-1.1.13.tar.gz
mkdir –p /data/aoa
cp aoa-linux-amd64-1.1.13/aoa /data/aoa/
nohup /data/aoa/aoa --datadir /data/aoa/aoa-data --port 30303 --rpc --rpcaddr 0.0.0.0 --rpcport 8545 &>> /data/aoa/aoa.log &
```
Attach the console
```
/data/aoa/aoa attach /data/aoa/aoa-data/aoa.ipc
```
Common console commond
```
aoa.blockNumber #get block height
aoa.getBlock(blockHashOrBlockNumber) #get block info
aoa.accounts #get accounts in wallet
aoa.getTransaction(transactionHash) #get transaction info
personal.newAccount(passphrase) #generate new accounts and store them in the keystore directory, encrypted with passphrase
personal.sendTransaction({from:'affress',to:'address',value:web3.toWei(100,'aoa'),action:0}, "password") #sent transaction
admin.startRPC("0.0.0.0", 8545) #start rpc by console
admin.stopRPC() #stop rpc by console
```
