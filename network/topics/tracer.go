package topics

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"go.uber.org/zap"
	"sync/atomic"
)

// pubsub tracer states
const (
	psTraceStateWithReporting uint32 = 0
	psTraceStateWithLogging   uint32 = 1
)

// psTracer helps to trace pubsub events
// it can run with logging in addition to reporting (on by default)
type psTracer struct {
	logger *zap.Logger
	state  uint32
}

// newTracer creates an instance of psTracer
func newTracer(logger *zap.Logger, withLogging bool) pubsub.EventTracer {
	state := psTraceStateWithReporting
	if withLogging {
		state = psTraceStateWithLogging
	}
	return &psTracer{logger: logger.With(zap.String("who", "pubsubTrace")), state: state}
}

// Trace handles events, implementation of pubsub.EventTracer
func (pst *psTracer) Trace(evt *ps_pb.TraceEvent) {
	pst.report(evt)
	if atomic.LoadUint32(&pst.state) < psTraceStateWithLogging {
		return
	}
	pst.log(evt)
}

// report reports metric
func (pst *psTracer) report(evt *ps_pb.TraceEvent) {
	metricsPubsubTrace.WithLabelValues(evt.GetType().String()).Inc()
}

// log prints event to log
func (pst *psTracer) log(evt *ps_pb.TraceEvent) {
	pst.logger.Debug("pubsub event",
		zap.String("type", evt.GetType().String()))
}
