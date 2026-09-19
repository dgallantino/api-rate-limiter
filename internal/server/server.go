package server

import (
	"context"
	"log/slog"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/stats"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	checkv1.UnimplementedCheckerServer
	cfg     *config.Config
	checker engine.Checker
	rec     *stats.Recorder
	log     *slog.Logger
}

func New(cfg *config.Config, checker engine.Checker, rec *stats.Recorder) *Server {
	if rec == nil {
		rec = stats.New()
	}
	return &Server{cfg: cfg, checker: checker, rec: rec}
}

func (s *Server) WithLogger(log *slog.Logger) *Server {
	s.log = log
	return s
}

func (s *Server) Check(ctx context.Context, req *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	if req.GetCost() < 0 {
		return nil, status.Error(codes.InvalidArgument, "cost must be >= 0")
	}
	policy := s.cfg.Lookup(req.GetKey())
	res, err := s.checker.Check(ctx, req.GetKey(), req.GetCost(), policy)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check: %v", err)
	}
	s.rec.Observe(req.GetKey(), res.Allowed, res.Remaining, policy.Limit, policy.Fail.String(), res.StoreFailed)
	if !res.Allowed && s.log != nil {
		s.log.Info("deny",
			"key", req.GetKey(),
			"remaining", res.Remaining,
			"retry_after_ms", res.RetryAfterMs,
			"fail", policy.Fail.String(),
		)
	}
	return &checkv1.CheckResponse{
		Allowed:      res.Allowed,
		Remaining:    res.Remaining,
		RetryAfterMs: res.RetryAfterMs,
	}, nil
}

func (s *Server) Stats(context.Context, *checkv1.StatsRequest) (*checkv1.StatsSnapshot, error) {
	snap := s.rec.Snapshot()
	keys := make([]*checkv1.KeyStat, len(snap.Keys))
	for i, k := range snap.Keys {
		keys[i] = &checkv1.KeyStat{
			Key:       k.Key,
			Used:      k.Used,
			Remaining: k.Remaining,
			Limit:     k.Limit,
			Fail:      k.Fail,
		}
	}
	return &checkv1.StatsSnapshot{
		Allowed: snap.Allowed,
		Blocked: snap.Blocked,
		Rps:     snap.RPS,
		RedisUp: snap.RedisUp,
		Keys:    keys,
	}, nil
}
