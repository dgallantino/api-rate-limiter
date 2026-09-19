package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/checkhttp"
	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine/slidingwindow"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/server"
	"github.com/dgallantino/api-rate-limiter/internal/stats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	redislog "github.com/redis/go-redis/v9/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	redislog.Disable()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	path := flag.String("config", "configs/check.example.yaml", "path to YAML or JSON config")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	defer rdb.Close()

	rec := stats.New()
	rec.SetOnRedisChange(func(up bool) {
		if up {
			slog.Info("redis up")
			return
		}
		slog.Info("redis down")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go stats.WatchRedis(ctx, func(c context.Context) error {
		return rdb.Ping(c).Err()
	}, rec, time.Second)

	if cfg.MetricsAddr != "" {
		reg := prometheus.NewRegistry()
		if err := rec.Register(reg); err != nil {
			slog.Error("metrics register", "err", err)
			os.Exit(1)
		}
		httpLn, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			slog.Error("listen", "addr", cfg.MetricsAddr, "err", err)
			os.Exit(1)
		}
		go func() {
			slog.Info("check listening", "http", cfg.MetricsAddr)
			if err := http.Serve(httpLn, checkhttp.Handler(reg)); err != nil {
				slog.Error("metrics serve", "err", err)
				os.Exit(1)
			}
		}()
	}

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		slog.Error("listen", "addr", cfg.ListenAddr, "err", err)
		os.Exit(1)
	}

	gs := grpc.NewServer()
	checkv1.RegisterCheckerServer(gs, server.New(cfg, slidingwindow.New(rdb), rec).WithLogger(slog.Default()))
	reflection.Register(gs)
	slog.Info("check listening", "grpc", cfg.ListenAddr)
	if err := gs.Serve(lis); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
