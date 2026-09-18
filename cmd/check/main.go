package main

import (
	"context"
	"flag"
	"log"
	"net"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine/slidingwindow"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/server"
	"github.com/dgallantino/api-rate-limiter/internal/stats"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	path := flag.String("config", "configs/check.example.yaml", "path to YAML or JSON config")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatal(err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	defer rdb.Close()

	rec := stats.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go stats.WatchRedis(ctx, func(c context.Context) error {
		return rdb.Ping(c).Err()
	}, rec, time.Second)

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatal(err)
	}

	gs := grpc.NewServer()
	checkv1.RegisterCheckerServer(gs, server.New(cfg, slidingwindow.New(rdb), rec))
	reflection.Register(gs)
	log.Printf("check listening on %s (h2c)", cfg.ListenAddr)
	log.Fatal(gs.Serve(lis))
}
