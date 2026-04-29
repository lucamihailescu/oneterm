package service

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/veops/oneterm/internal/model"
	gsession "github.com/veops/oneterm/internal/session"
	"github.com/veops/oneterm/pkg/logger"
)

// JITSweepInterval is how often the just-in-time sweeper looks for expired
// access requests. 30 s gives ~30 s worst-case post-expiry session lifetime,
// which is acceptable for compliance and cheap on the database.
const JITSweepInterval = 30 * time.Second

var (
	jitSweeperOnce sync.Once
	jitStopCh      = make(chan struct{})
)

// StartJITSweeper launches the background goroutine that, every
// JITSweepInterval, finds approved AccessRequests whose expires_at has passed
// and (a) flips them to status=expired and (b) force-closes any online
// session that was authorized by them.
//
// Idempotent: safe to call from initServices() once. If called more than once
// the extra calls are no-ops.
func StartJITSweeper(ctx context.Context) {
	jitSweeperOnce.Do(func() {
		go runJITSweeper(ctx)
		logger.L().Info("JIT sweeper started", zap.Duration("interval", JITSweepInterval))
	})
}

// StopJITSweeper signals the sweeper to exit on its next tick. Mainly used
// for tests; production processes exit by signal.
func StopJITSweeper() { close(jitStopCh) }

func runJITSweeper(ctx context.Context) {
	tk := time.NewTicker(JITSweepInterval)
	defer tk.Stop()
	svc := NewAccessRequestService()

	for {
		select {
		case <-jitStopCh:
			return
		case <-ctx.Done():
			return
		case <-tk.C:
			expired, err := svc.SweepExpired(ctx)
			if err != nil {
				logger.L().Warn("jit sweep query failed", zap.Error(err))
				continue
			}
			if len(expired) == 0 {
				continue
			}
			closeSessionsForRequests(expired)
		}
	}
}

// closeSessionsForRequests scans the live online-session map and signals
// CloseChan on any session whose AccessRequestId matches an expired request.
// We index expired by id first so the scan stays O(N) over live sessions
// even when many requests expire in the same tick.
func closeSessionsForRequests(expired []*model.AccessRequest) {
	expiredById := make(map[int]struct{}, len(expired))
	for _, ar := range expired {
		expiredById[ar.Id] = struct{}{}
	}

	closed := 0
	gsession.GetOnlineSession().Range(func(_, v any) bool {
		sess, ok := v.(*gsession.Session)
		if !ok || sess == nil || sess.AccessRequestId == 0 {
			return true
		}
		if _, hit := expiredById[sess.AccessRequestId]; !hit {
			return true
		}
		// Use a non-blocking send so a session whose handler is wedged
		// doesn't stall the sweeper. Worst case the session lingers until
		// natural close — the underlying access grant is already revoked
		// in the DB so a subsequent reconnect will be denied.
		select {
		case sess.Chans.CloseChan <- "jit-expired":
			closed++
		default:
			logger.L().Warn("jit sweeper: CloseChan full, skipping",
				zap.String("sessionId", sess.SessionId))
		}
		return true
	})
	if closed > 0 {
		logger.L().Info("jit sweeper closed expired sessions",
			zap.Int("count", closed),
			zap.Int("expired_requests", len(expired)))
	}
}
