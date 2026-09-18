package httplimit

import (
	"context"
	"net/http"
	"strconv"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CostFunc func(*http.Request) int64

type CheckClient interface {
	Check(ctx context.Context, in *checkv1.CheckRequest, opts ...grpc.CallOption) (*checkv1.CheckResponse, error)
}

type Options struct {
	Client   CheckClient
	KeyFunc  KeyFunc
	Cost     int64
	CostFunc CostFunc
	Fail     FailMode
}

func Middleware(opts Options) func(http.Handler) http.Handler {
	if opts.Client == nil {
		panic("httplimit: Client is required")
	}
	keyFn := opts.KeyFunc
	if keyFn == nil {
		keyFn = KeyFromHeader("X-API-Key")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, err := keyFn(r)
			if err != nil {
				writeBadRequest(w)
				return
			}
			cost := opts.Cost
			if opts.CostFunc != nil {
				cost = opts.CostFunc(r)
			}
			res, err := opts.Client.Check(r.Context(), &checkv1.CheckRequest{Key: key, Cost: cost})
			if err != nil {
				if status.Code(err) == codes.InvalidArgument {
					writeBadRequest(w)
					return
				}
				if opts.Fail == FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				WriteDeny(w, 0, 0)
				return
			}
			if !res.Allowed {
				WriteDeny(w, res.Remaining, res.RetryAfterMs)
				return
			}
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
			next.ServeHTTP(w, r)
		})
	}
}
