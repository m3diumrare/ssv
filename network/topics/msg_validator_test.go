package topics

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	genesisvalidation "github.com/ssvlabs/ssv/message/validation/genesis"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
)

func TestMsgValidator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	ns, err := operatorstorage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, 1),
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Status: v1.ValidatorStateActiveOngoing,
			},
			Liquidated: false,
		},
	}
	require.NoError(t, ns.Shares().Save(nil, share))

	mv := genesisvalidation.New(networkconfig.TestNetwork, genesisvalidation.WithNodeStorage(ns))
	require.NotNil(t, mv)

	slot := networkconfig.TestNetwork.Beacon.GetBeaconNetwork().EstimatedCurrentSlot()

	t.Run("valid consensus msg", func(t *testing.T) {
		ssvMsg, err := dummySSVConsensusMsg(share.ValidatorPubKey, qbft.Height(slot))
		require.NoError(t, err)

		_, skByte, err := rsaencryption.GenerateKeys()
		require.NoError(t, err)
		operatorPrivateKey, err := rsaencryption.PemToPrivateKey(skByte)
		require.NoError(t, err)

		operatorId := uint64(1)

		operatorPubKey, err := rsaencryption.ExtractPublicKey(&operatorPrivateKey.PublicKey)
		require.NoError(t, err)

		od := &storage.OperatorData{
			PublicKey:    []byte(operatorPubKey),
			OwnerAddress: common.Address{},
			ID:           operatorId,
		}

		found, err := ns.SaveOperatorData(nil, od)
		require.False(t, found)
		require.NoError(t, err)

		encodedMsg, err := commons.EncodeNetworkMsg(ssvMsg)
		require.NoError(t, err)

		hash := sha256.Sum256(encodedMsg)
		signature, err := rsa.SignPKCS1v15(nil, operatorPrivateKey, crypto.SHA256, hash[:])
		require.NoError(t, err)

		var sig []byte
		copy(sig[:], signature)

		packedPubSubMsgPayload := genesisspectypes.EncodeSignedSSVMessage(encodedMsg, operatorId, sig)
		topicID := commons.ValidatorTopicID(ssvMsg.GetID().GetDutyExecutorID())

		pmsg := &pubsub.Message{
			Message: &pspb.Message{
				Topic: &topicID[0],
				Data:  packedPubSubMsgPayload,
			},
		}
		res := mv.Validate(context.Background(), "16Uiu2HAkyWQyCb6reWXGQeBUt9EXArk6h3aq3PsFMwLNq3pPGH1r", pmsg)
		require.Equal(t, pubsub.ValidationAccept, res)
	})

	t.Run("empty message", func(t *testing.T) {
		pmsg := newPBMsg([]byte{}, "xxx", []byte{})
		res := mv.Validate(context.Background(), "xxxx", pmsg)
		require.Equal(t, pubsub.ValidationReject, res)
	})
}

func newPBMsg(data []byte, topic string, from []byte) *pubsub.Message {
	pmsg := &pubsub.Message{
		Message: &ps_pb.Message{},
	}
	pmsg.Data = data
	pmsg.Topic = &topic
	pmsg.From = from
	return pmsg
}

func dummySSVConsensusMsg(pk spectypes.ValidatorPK, height qbft.Height) (*spectypes.SSVMessage, error) {
	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType(), pk[:], spectypes.RoleCommittee)
	ks := spectestingutils.Testing4SharesSet()

	validSignedMessage := spectestingutils.TestingRoundChangeMessageWithHeightAndIdentifier(ks.OperatorKeys[1], 1, height, id[:])

	return &spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   id,
		Data:    validSignedMessage.SSVMessage.Data,
	}, nil
}
