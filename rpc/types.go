package rpc

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"gopkg.in/fatih/set.v0"
)

type API struct {
	Namespace string      
	Version   string      
	Service   interface{} 
	Public    bool        
}

type callback struct {
	rcvr        reflect.Value  
	method      reflect.Method 
	argTypes    []reflect.Type 
	hasCtx      bool           
	errPos      int            
	isSubscribe bool           
}

type service struct {
	name          string        
	typ           reflect.Type  
	callbacks     callbacks     
	subscriptions subscriptions 
}

type serverRequest struct {
	id            interface{}
	svcname       string
	callb         *callback
	args          []reflect.Value
	isUnsubscribe bool
	err           Error
}

type serviceRegistry map[string]*service 
type callbacks map[string]*callback      
type subscriptions map[string]*callback  

type Server struct {
	services serviceRegistry

	run      int32
	codecsMu sync.Mutex
	codecs   *set.Set
}

type rpcRequest struct {
	service  string
	method   string
	id       interface{}
	isPubSub bool
	params   interface{}
	err      Error 
}

type Error interface {
	Error() string  
	ErrorCode() int 
}

type ServerCodec interface {

	ReadRequestHeaders() ([]rpcRequest, bool, Error)

	ParseRequestArguments(argTypes []reflect.Type, params interface{}) ([]reflect.Value, Error)

	CreateResponse(id interface{}, reply interface{}) interface{}

	CreateErrorResponse(id interface{}, err Error) interface{}

	CreateErrorResponseWithInfo(id interface{}, err Error, info interface{}) interface{}

	CreateNotification(id, namespace string, event interface{}) interface{}

	Write(msg interface{}) error

	Close()

	Closed() <-chan interface{}
}

type BlockNumber int64

const (
	PendingBlockNumber  = BlockNumber(-2)
	LatestBlockNumber   = BlockNumber(-1)
	EarliestBlockNumber = BlockNumber(0)
)

func (bn *BlockNumber) UnmarshalJSON(data []byte) error {
	input := strings.TrimSpace(string(data))
	if len(input) >= 2 && input[0] == '"' && input[len(input)-1] == '"' {
		input = input[1 : len(input)-1]
	}

	switch input {
	case "earliest":
		*bn = EarliestBlockNumber
		return nil
	case "latest":
		*bn = LatestBlockNumber
		return nil
	case "pending":
		*bn = PendingBlockNumber
		return nil
	}

	blckNum, err := hexutil.DecodeUint64(input)
	if err != nil {
		return err
	}
	if blckNum > math.MaxInt64 {
		return fmt.Errorf("Blocknumber too high")
	}

	*bn = BlockNumber(blckNum)
	return nil
}

func (bn BlockNumber) Int64() int64 {
	return (int64)(bn)
}
