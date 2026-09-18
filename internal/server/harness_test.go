package server

import (
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine/slidingwindow"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type testEnv struct {
	client checkv1.CheckerClient
	mr     *miniredis.Miniredis
	stop   func()
}

func startTestServer(t *testing.T, cfg *config.Config) *testEnv {
	t.Helper()
	if cfg.Default.Window == 0 {
		cfg.Default.Window = time.Minute
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	checkv1.RegisterCheckerServer(gs, New(cfg, slidingwindow.New(rdb), nil))
	go gs.Serve(lis)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		client: checkv1.NewCheckerClient(conn),
		mr:     mr,
		stop: func() {
			gs.Stop()
			_ = conn.Close()
			_ = rdb.Close()
			_ = lis.Close()
		},
	}
}
