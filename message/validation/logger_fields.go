package validation

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/logging/fields"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// ConsensusFields provides details about the consensus for a message. It's used for logging and metrics.
type ConsensusFields struct {
	Round           specqbft.Round
	QBFTMessageType specqbft.MessageType
}

// LoggerFields provides details about a message. It's used for logging and metrics.
type LoggerFields struct {
	DutyExecutorID []byte
	Role           spectypes.RunnerRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	Consensus      *ConsensusFields
	OperatorIDs    []spectypes.OperatorID
}

// AsZapFields returns zap logging fields for the descriptor.
func (d LoggerFields) AsZapFields() []zapcore.Field {
	result := []zapcore.Field{
		fields.DutyExecutorID(d.DutyExecutorID),
		fields.Role(d.Role),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(d.SSVMessageType)),
		fields.Slot(d.Slot),
		fields.OperatorIDs(d.OperatorIDs),
	}

	if d.Consensus != nil {
		result = append(result,
			fields.Round(d.Consensus.Round),
			zap.String("qbft_message_type", ssvmessage.QBFTMsgTypeToString(d.Consensus.QBFTMessageType)),
		)
	}

	return result
}

func (mv *messageValidator) buildLoggerFields(decodedMessage *queue.DecodedSSVMessage) *LoggerFields {
	descriptor := &LoggerFields{
		Consensus: &ConsensusFields{},
	}

	if decodedMessage == nil {
		return descriptor
	}

	descriptor.DutyExecutorID = decodedMessage.GetID().GetDutyExecutorID()
	descriptor.Role = decodedMessage.GetID().GetRoleType()
	descriptor.SSVMessageType = decodedMessage.GetType()
	descriptor.OperatorIDs = decodedMessage.SignedSSVMessage.OperatorIDs

	switch m := decodedMessage.Body.(type) {
	case *specqbft.Message:
		if m != nil {
			descriptor.Slot = phase0.Slot(m.Height)
			descriptor.Consensus.Round = m.Round
			descriptor.Consensus.QBFTMessageType = m.MsgType
		}
	case *spectypes.PartialSignatureMessages:
		if m != nil {
			descriptor.Slot = m.Slot
		}
	}

	return descriptor
}
