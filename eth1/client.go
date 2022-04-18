package eth1

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/prysmaticlabs/prysm/async/event"
	"math/big"
	"time"
)

// Options configurations related to eth1
type Options struct {
	ETH1Addr              string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"ETH1 node WebSocket address"`
	ETH1SyncOffset        string        `yaml:"ETH1SyncOffset" env:"ETH_1_SYNC_OFFSET" env-description:"block number to start the sync from"`
	ETH1ConnectionTimeout time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"eth1 node connection timeout"`
	RegistryContractAddr  string        `yaml:"RegistryContractAddr" env:"REGISTRY_CONTRACT_ADDR_KEY" env-default:"0x687fb596F3892904F879118e2113e1EEe8746C2E" env-description:"registry contract address"`
	RegistryContractABI   string        `yaml:"RegistryContractABI" env:"REGISTRY_CONTRACT_ABI" env-description:"registry contract abi json file"`
	CleanRegistryData     bool          `yaml:"CleanRegistryData" env:"CLEAN_REGISTRY_DATA" env-default:"false" env-description:"cleans registry contract data (validator shares) and forces re-sync"`
	AbiVersion            Version       `yaml:"AbiVersion" env:"ABI_VERSION" env-default:"0" env-description:"smart contract abi version (format)"`
}

// Event represents an eth1 event log in the system
type Event struct {
	// Log is the raw event log
	Log types.Log
	// Name is the event name used for internal representation.
	Name string
	// Data is the parsed event
	Data interface{}
}

// SyncEndedEvent meant to notify an observer that the sync is over
type SyncEndedEvent struct {
	// Success returns true if the sync went well (all events were parsed)
	Success bool
	// Logs is the actual logs that we got from eth1
	Logs []types.Log
}

// Client represents the required interface for eth1 client
type Client interface {
	EventsFeed() *event.Feed
	Start() error
	Sync(fromBlock *big.Int) error
}
