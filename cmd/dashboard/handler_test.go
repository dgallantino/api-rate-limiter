package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubStats struct {
	snap     *checkv1.StatsSnapshot
	err      error
	policies *checkv1.ListPoliciesResponse
	polErr   error
	set      *checkv1.SetLimitRequest
	setRes   *checkv1.SetLimitResponse
	setErr   error
}

func (s stubStats) Stats(context.Context, *checkv1.StatsRequest, ...grpc.CallOption) (*checkv1.StatsSnapshot, error) {
	return s.snap, s.err
}

func (s stubStats) ListPolicies(context.Context, *checkv1.ListPoliciesRequest, ...grpc.CallOption) (*checkv1.ListPoliciesResponse, error) {
	return s.policies, s.polErr
}

func (s *stubStats) SetLimit(_ context.Context, in *checkv1.SetLimitRequest, _ ...grpc.CallOption) (*checkv1.SetLimitResponse, error) {
	s.set = in
	return s.setRes, s.setErr
}

func TestStatsJSON(t *testing.T) {
	pages := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	h := newMux(&stubStats{snap: &checkv1.StatsSnapshot{
		Allowed: 3,
		Blocked: 1,
		Rps:     4,
		RedisUp: true,
		Keys: []*checkv1.KeyStat{{
			Key:       "free:demo",
			Used:      3,
			Remaining: 17,
			Limit:     20,
			Fail:      "open",
		}},
	}}, pages)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var got statsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Allowed != 3 || got.Blocked != 1 || got.RPS != 4 || !got.RedisUp || len(got.Keys) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Keys[0].Key != "free:demo" || got.Keys[0].Fail != "open" || got.Keys[0].Used != 3 {
		t.Fatalf("key: %+v", got.Keys[0])
	}
}

func TestStatsUnreachable(t *testing.T) {
	h := newMux(&stubStats{err: status.Error(codes.Unavailable, "down")}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code=%d", rec.Code)
	}
	var got statsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "check unreachable" {
		t.Fatalf("%+v", got)
	}
}

func TestIndex(t *testing.T) {
	pages := fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	h := newMux(&stubStats{}, pages)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>dash</html>" {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("ctype=%s", rec.Header().Get("Content-Type"))
	}
}

func TestPoliciesAndSetLimit(t *testing.T) {
	stub := &stubStats{
		policies: &checkv1.ListPoliciesResponse{Rules: []*checkv1.PolicyRule{{
			Target: checkv1.PolicyTarget_POLICY_TARGET_PREFIX,
			Name:   "free:",
			Limit:  20,
			Window: "1m",
			Fail:   "open",
		}}},
		setRes: &checkv1.SetLimitResponse{Rule: &checkv1.PolicyRule{
			Target: checkv1.PolicyTarget_POLICY_TARGET_PREFIX,
			Name:   "free:",
			Limit:  30,
			Window: "1m",
			Fail:   "open",
		}},
	}
	h := newMux(stub, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/policies", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed policiesJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Rules) != 1 || listed.Rules[0].Target != "prefix" || listed.Rules[0].Name != "free:" || listed.Rules[0].Limit != 20 {
		t.Fatalf("%+v", listed)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/limits", strings.NewReader(`{"target":"prefix","name":"free:","limit":30}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if stub.set.GetTarget() != checkv1.PolicyTarget_POLICY_TARGET_PREFIX || stub.set.GetName() != "free:" || stub.set.GetLimit() != 30 {
		t.Fatalf("set: %+v", stub.set)
	}
	var updated policyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Limit != 30 || updated.Error != "" {
		t.Fatalf("%+v", updated)
	}
}

func TestSetLimitRejected(t *testing.T) {
	h := newMux(&stubStats{setErr: status.Error(codes.InvalidArgument, "config: limit must be > 0")}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/limits", strings.NewReader(`{"target":"default","limit":0}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
	var got policyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "config: limit must be > 0" {
		t.Fatalf("%+v", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/limits", strings.NewReader(`{"target":"nope","limit":1}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown target code=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h = newMux(&stubStats{setErr: status.Error(codes.Unavailable, "down")}, fstest.MapFS{})
	req = httptest.NewRequest(http.MethodPost, "/limits", strings.NewReader(`{"target":"default","limit":1}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("unreachable code=%d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "check unreachable" {
		t.Fatalf("%+v", got)
	}
}
