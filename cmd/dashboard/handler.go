package main

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type checkClient interface {
	Stats(ctx context.Context, in *checkv1.StatsRequest, opts ...grpc.CallOption) (*checkv1.StatsSnapshot, error)
	ListPolicies(ctx context.Context, in *checkv1.ListPoliciesRequest, opts ...grpc.CallOption) (*checkv1.ListPoliciesResponse, error)
	SetLimit(ctx context.Context, in *checkv1.SetLimitRequest, opts ...grpc.CallOption) (*checkv1.SetLimitResponse, error)
}

type keyJSON struct {
	Key       string `json:"key"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
	Limit     int64  `json:"limit"`
	Fail      string `json:"fail"`
}

type statsJSON struct {
	Allowed int64     `json:"allowed"`
	Blocked int64     `json:"blocked"`
	RPS     int64     `json:"rps"`
	RedisUp bool      `json:"redis_up"`
	Keys    []keyJSON `json:"keys"`
	Error   string    `json:"error,omitempty"`
}

type policyJSON struct {
	Target string `json:"target"`
	Name   string `json:"name"`
	Limit  int64  `json:"limit"`
	Window string `json:"window,omitempty"`
	Fail   string `json:"fail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type policiesJSON struct {
	Rules []policyJSON `json:"rules"`
	Error string       `json:"error,omitempty"`
}

func newMux(client checkClient, pages fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		serveStats(w, r, client)
	})
	mux.HandleFunc("GET /policies", func(w http.ResponseWriter, r *http.Request) {
		servePolicies(w, r, client)
	})
	mux.HandleFunc("POST /limits", func(w http.ResponseWriter, r *http.Request) {
		serveSetLimit(w, r, client)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(pages, "index.html")
		if err != nil {
			http.Error(w, "index not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	return mux
}

func serveStats(w http.ResponseWriter, r *http.Request, client checkClient) {
	w.Header().Set("Content-Type", "application/json")
	snap, err := client.Stats(r.Context(), &checkv1.StatsRequest{})
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(statsJSON{Error: "check unreachable"})
		return
	}
	keys := make([]keyJSON, 0, len(snap.GetKeys()))
	for _, k := range snap.GetKeys() {
		keys = append(keys, keyJSON{
			Key:       k.GetKey(),
			Used:      k.GetUsed(),
			Remaining: k.GetRemaining(),
			Limit:     k.GetLimit(),
			Fail:      k.GetFail(),
		})
	}
	_ = json.NewEncoder(w).Encode(statsJSON{
		Allowed: snap.GetAllowed(),
		Blocked: snap.GetBlocked(),
		RPS:     snap.GetRps(),
		RedisUp: snap.GetRedisUp(),
		Keys:    keys,
	})
}

func servePolicies(w http.ResponseWriter, r *http.Request, client checkClient) {
	w.Header().Set("Content-Type", "application/json")
	res, err := client.ListPolicies(r.Context(), &checkv1.ListPoliciesRequest{})
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(policiesJSON{Error: "check unreachable"})
		return
	}
	rules := make([]policyJSON, 0, len(res.GetRules()))
	for _, rule := range res.GetRules() {
		rules = append(rules, policyJSON{
			Target: targetString(rule.GetTarget()),
			Name:   rule.GetName(),
			Limit:  rule.GetLimit(),
			Window: rule.GetWindow(),
			Fail:   rule.GetFail(),
		})
	}
	_ = json.NewEncoder(w).Encode(policiesJSON{Rules: rules})
}

func serveSetLimit(w http.ResponseWriter, r *http.Request, client checkClient) {
	w.Header().Set("Content-Type", "application/json")
	var body policyJSON
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(policyJSON{Error: "bad request"})
		return
	}
	target, ok := parseTarget(body.Target)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(policyJSON{Error: "unknown policy"})
		return
	}
	res, err := client.SetLimit(r.Context(), &checkv1.SetLimitRequest{
		Target: target,
		Name:   body.Name,
		Limit:  body.Limit,
	})
	if err != nil {
		if status.Code(err) == codes.InvalidArgument {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(policyJSON{Error: status.Convert(err).Message()})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(policyJSON{Error: "check unreachable"})
		return
	}
	rule := res.GetRule()
	_ = json.NewEncoder(w).Encode(policyJSON{
		Target: targetString(rule.GetTarget()),
		Name:   rule.GetName(),
		Limit:  rule.GetLimit(),
		Window: rule.GetWindow(),
		Fail:   rule.GetFail(),
	})
}

func targetString(target checkv1.PolicyTarget) string {
	switch target {
	case checkv1.PolicyTarget_POLICY_TARGET_DEFAULT:
		return "default"
	case checkv1.PolicyTarget_POLICY_TARGET_KEY:
		return "key"
	case checkv1.PolicyTarget_POLICY_TARGET_PREFIX:
		return "prefix"
	default:
		return ""
	}
}

func parseTarget(target string) (checkv1.PolicyTarget, bool) {
	switch target {
	case "default":
		return checkv1.PolicyTarget_POLICY_TARGET_DEFAULT, true
	case "key":
		return checkv1.PolicyTarget_POLICY_TARGET_KEY, true
	case "prefix":
		return checkv1.PolicyTarget_POLICY_TARGET_PREFIX, true
	default:
		return checkv1.PolicyTarget_POLICY_TARGET_UNSPECIFIED, false
	}
}
