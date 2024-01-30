package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/operator/slotticker"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

const (
	// DataVersionNil is just a placeholder for a nil data version.
	// Don't check for it, check for errors or nil data instead.
	DataVersionNil spec.DataVersion = math.MaxUint64

	// Client timeouts.
	commonTimeout = time.Second * 5
	maxTimeout    = time.Second * 120
)

type beaconNodeStatus int32

var (
	allMetrics = []prometheus.Collector{
		metricsBeaconNodeStatus,
		metricsBeaconDataRequest,
	}
	metricsBeaconNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_beacon_status",
		Help: "Status of the connected beacon node",
	})

	// metricsBeaconDataRequest is located here to avoid including waiting for 1/3 or 2/3 of slot time into request duration.
	metricsBeaconDataRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_beacon_data_request_duration_seconds",
		Help:    "Beacon data request duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})

	metricsAttesterDataRequest                  = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleAttester.String())
	metricsAggregatorDataRequest                = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleAggregator.String())
	metricsProposerDataRequest                  = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleProposer.String())
	metricsSyncCommitteeDataRequest             = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleSyncCommittee.String())
	metricsSyncCommitteeContributionDataRequest = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleSyncCommitteeContribution.String())

	statusUnknown beaconNodeStatus = 0
	statusSyncing beaconNodeStatus = 1
	statusOK      beaconNodeStatus = 2
)

func init() {
	logger := zap.L()
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			logger.Debug("could not register prometheus collector")
		}
	}
}

// NodeClient is the type of the Beacon node.
type NodeClient string

const (
	NodeLighthouse NodeClient = "lighthouse"
	NodePrysm      NodeClient = "prysm"
	NodeUnknown    NodeClient = "unknown"
)

// ParseNodeClient derives the client from node's version string.
func ParseNodeClient(version string) NodeClient {
	version = strings.ToLower(version)
	switch {
	case strings.Contains(version, "lighthouse"):
		return NodeLighthouse
	case strings.Contains(version, "prysm"):
		return NodePrysm
	default:
		return NodeUnknown
	}
}

// Client defines all go-eth2-client interfaces used in ssv
type Client interface {
	eth2client.Service
	eth2client.NodeVersionProvider
	eth2client.NodeClientProvider

	eth2client.AttestationDataProvider
	eth2client.AttestationsSubmitter
	eth2client.AggregateAttestationProvider
	eth2client.AggregateAttestationsSubmitter
	eth2client.BeaconCommitteeSubscriptionsSubmitter
	eth2client.SyncCommitteeSubscriptionsSubmitter
	eth2client.AttesterDutiesProvider
	eth2client.ProposerDutiesProvider
	eth2client.SyncCommitteeDutiesProvider
	eth2client.NodeSyncingProvider
	eth2client.ProposalProvider
	eth2client.ProposalSubmitter
	eth2client.BlindedProposalProvider
	eth2client.BlindedProposalSubmitter
	eth2client.DomainProvider
	eth2client.SyncCommitteeMessagesSubmitter
	eth2client.BeaconBlockRootProvider
	eth2client.SyncCommitteeContributionProvider
	eth2client.SyncCommitteeContributionsSubmitter
	eth2client.ValidatorsProvider
	eth2client.ProposalPreparationsSubmitter
	eth2client.EventsProvider
	eth2client.ValidatorRegistrationsSubmitter
	eth2client.VoluntaryExitSubmitter
}

type NodeClientProvider interface {
	NodeClient() NodeClient
}

var _ NodeClientProvider = (*goClient)(nil)

// goClient implementing Beacon struct
type goClient struct {
	log                  *zap.Logger
	ctx                  context.Context
	network              beaconprotocol.Network
	client               Client
	nodeVersion          string
	nodeClient           NodeClient
	graffiti             []byte
	gasLimit             uint64
	operatorID           spectypes.OperatorID
	registrationMu       sync.Mutex
	registrationLastSlot phase0.Slot
	registrationCache    map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration
	commonTimeout        time.Duration
	maxTimeout           time.Duration
}

// New init new client and go-client instance
func New(logger *zap.Logger, opt beaconprotocol.Options, operatorID spectypes.OperatorID, slotTickerProvider slotticker.Provider) (beaconprotocol.BeaconNode, error) {

	logger.Info("consensus client: connecting", fields.Address(opt.BeaconNodeAddr), fields.Network(string(opt.Network.BeaconNetwork)))

	httpClient, err := eth2clienthttp.New(opt.Context,
		// WithAddress supplies the address of the beacon node, in host:port format.
		eth2clienthttp.WithAddress(opt.BeaconNodeAddr),
		// LogLevel supplies the level of logging to carry out.
		eth2clienthttp.WithLogLevel(zerolog.DebugLevel),
		eth2clienthttp.WithTimeout(commonTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create http client: %w", err)
	}

	client := &goClient{
		log:               logger,
		ctx:               opt.Context,
		network:           opt.Network,
		client:            httpClient.(*eth2clienthttp.Service),
		graffiti:          opt.Graffiti,
		gasLimit:          opt.GasLimit,
		operatorID:        operatorID,
		registrationCache: map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration{},
		commonTimeout:     commonTimeout,
		maxTimeout:        maxTimeout,
	}

	// Get the node's version and client.
	nodeVersionResp, err := client.client.NodeVersion(opt.Context, &api.NodeVersionOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node version: %w", err)
	}
	if nodeVersionResp == nil {
		return nil, fmt.Errorf("node version response is nil")
	}
	client.nodeVersion = nodeVersionResp.Data
	client.nodeClient = ParseNodeClient(nodeVersionResp.Data)

	logger.Info("consensus client connected",
		fields.Name(httpClient.Name()),
		fields.Address(httpClient.Address()),
		zap.String("client", string(client.nodeClient)),
		zap.String("version", client.nodeVersion),
	)

	// If it's a Prysm, check that the debug endpoints are enabled, otherwise
	// the Validators method might fail when calling BeaconState.
	// TODO: remove this once Prysm enables debug endpoints by default.
	if client.nodeClient == NodePrysm {
		if err := client.checkPrysmDebugEndpoints(); err != nil {
			return nil, err
		}
	}

	// Start registration submitter.
	go client.registrationSubmitter(slotTickerProvider)

	return client, nil
}

func (gc *goClient) NodeClient() NodeClient {
	return gc.nodeClient
}

// Healthy returns if beacon node is currently healthy: responds to requests, not in the syncing state, not optimistic
// (for optimistic see https://github.com/ethereum/consensus-specs/blob/dev/sync/optimistic.md#block-production).
func (gc *goClient) Healthy(ctx context.Context) error {
	nodeSyncingResp, err := gc.client.NodeSyncing(ctx, &api.NodeSyncingOpts{})
	if err != nil {
		// TODO: get rid of global variable, pass metrics to goClient
		metricsBeaconNodeStatus.Set(float64(statusUnknown))
		return fmt.Errorf("failed to obtain node syncing status: %w", err)
	}
	if nodeSyncingResp == nil {
		metricsBeaconNodeStatus.Set(float64(statusUnknown))
		return fmt.Errorf("node syncing response is nil")
	}
	if nodeSyncingResp.Data == nil {
		metricsBeaconNodeStatus.Set(float64(statusUnknown))
		return fmt.Errorf("node syncing data is nil")
	}
	syncState := nodeSyncingResp.Data

	// TODO: also check if syncState.ElOffline when github.com/attestantio/go-eth2-client supports it
	metricsBeaconNodeStatus.Set(float64(statusSyncing))
	if syncState.IsSyncing {
		return fmt.Errorf("syncing")
	}
	if syncState.IsOptimistic {
		return fmt.Errorf("optimistic")
	}

	metricsBeaconNodeStatus.Set(float64(statusOK))
	return nil
}

// GetBeaconNetwork returns the beacon network the node is on
func (gc *goClient) GetBeaconNetwork() spectypes.BeaconNetwork {
	return gc.network.BeaconNetwork
}

// SlotStartTime returns the start time in terms of its unix epoch
// value.
func (gc *goClient) slotStartTime(slot phase0.Slot) time.Time {
	duration := time.Second * time.Duration(uint64(slot)*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(gc.network.MinGenesisTime()), 0).Add(duration)
	return startTime
}

func (gc *goClient) Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error {
	return gc.client.Events(ctx, topics, handler)
}

func (gc *goClient) checkPrysmDebugEndpoints() error {
	start := time.Now()
	address := strings.TrimSuffix(gc.client.Address(), "/")
	if !strings.HasPrefix(address, "http") {
		address = fmt.Sprintf("http://%s", address)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commonTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/eth/v2/debug/fork_choice", address), nil)
	if err != nil {
		return fmt.Errorf("failed to create fork choice request: %w", err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("failed to get fork choice: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Prysm node doesn't have debug endpoints enabled, please enable them with --enable-debug-rpc-endpoints")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get fork choice: %s", resp.Status)
	}
	var data struct {
		JustifiedCheckpoint struct {
			Root phase0.Root `json:"root"`
		} `json:"justified_checkpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode fork choice response: %w", err)
	}
	if data.JustifiedCheckpoint.Root == (phase0.Root{}) {
		return fmt.Errorf("no justified checkpoint found")
	}
	gc.log.Debug("Prysm debug endpoints are enabled", zap.Duration("took", time.Since(start)))
	return nil
}
