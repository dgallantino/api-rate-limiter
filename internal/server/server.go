package server

import (
	"context"
	"errors"
	"fmt"
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
	store   *config.Store
	checker engine.Checker
	rec     *stats.Recorder
	log     *slog.Logger
}

func New(store *config.Store, checker engine.Checker, rec *stats.Recorder) *Server {
	if store == nil {
		store = config.NewStore(nil)
	}
	if rec == nil {
		rec = stats.New()
	}
	return &Server{store: store, checker: checker, rec: rec}
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
	policy := s.store.Lookup(req.GetKey())
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

func (s *Server) ListPolicies(context.Context, *checkv1.ListPoliciesRequest) (*checkv1.ListPoliciesResponse, error) {
	rules := s.store.List()
	out := make([]*checkv1.PolicyRule, len(rules))
	for i, rule := range rules {
		out[i] = toProtoRule(rule)
	}
	return &checkv1.ListPoliciesResponse{Rules: out}, nil
}

func (s *Server) SetLimit(_ context.Context, req *checkv1.SetLimitRequest) (*checkv1.SetLimitResponse, error) {
	target, err := fromProtoTarget(req.GetTarget())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	rule, err := s.store.SetLimit(target, req.GetName(), req.GetLimit())
	if err != nil {
		if errors.Is(err, config.ErrInvalidLimit) || errors.Is(err, config.ErrUnknownPolicy) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "set limit: %v", err)
	}
	if s.log != nil {
		s.log.Info("set limit", "target", string(rule.Target), "name", rule.Name, "limit", rule.Policy.Limit)
	}
	return &checkv1.SetLimitResponse{Rule: toProtoRule(rule)}, nil
}

func toProtoRule(rule config.Rule) *checkv1.PolicyRule {
	return &checkv1.PolicyRule{
		Target: toProtoTarget(rule.Target),
		Name:   rule.Name,
		Limit:  rule.Policy.Limit,
		Window: rule.Policy.WindowString(),
		Fail:   rule.Policy.Fail.String(),
	}
}

func toProtoTarget(target config.Target) checkv1.PolicyTarget {
	switch target {
	case config.TargetDefault:
		return checkv1.PolicyTarget_POLICY_TARGET_DEFAULT
	case config.TargetKey:
		return checkv1.PolicyTarget_POLICY_TARGET_KEY
	case config.TargetPrefix:
		return checkv1.PolicyTarget_POLICY_TARGET_PREFIX
	default:
		return checkv1.PolicyTarget_POLICY_TARGET_UNSPECIFIED
	}
}

func fromProtoTarget(target checkv1.PolicyTarget) (config.Target, error) {
	switch target {
	case checkv1.PolicyTarget_POLICY_TARGET_DEFAULT:
		return config.TargetDefault, nil
	case checkv1.PolicyTarget_POLICY_TARGET_KEY:
		return config.TargetKey, nil
	case checkv1.PolicyTarget_POLICY_TARGET_PREFIX:
		return config.TargetPrefix, nil
	default:
		return "", fmt.Errorf("config: %w", config.ErrUnknownPolicy)
	}
}
