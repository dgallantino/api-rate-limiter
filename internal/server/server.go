package server

import (
	"context"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	checkv1.UnimplementedCheckerServer
	cfg     *config.Config
	checker engine.Checker
}

func New(cfg *config.Config, checker engine.Checker) *Server {
	return &Server{cfg: cfg, checker: checker}
}

func (s *Server) Check(ctx context.Context, req *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	if req.GetCost() < 0 {
		return nil, status.Error(codes.InvalidArgument, "cost must be >= 0")
	}
	res, err := s.checker.Check(ctx, req.GetKey(), req.GetCost(), s.cfg.Lookup(req.GetKey()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check: %v", err)
	}
	return &checkv1.CheckResponse{
		Allowed:      res.Allowed,
		Remaining:    res.Remaining,
		RetryAfterMs: res.RetryAfterMs,
	}, nil
}
