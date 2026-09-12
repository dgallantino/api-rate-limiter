package main

import (
	"flag"
	"log"
	"net"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine/slidingwindow"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/server"
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

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatal(err)
	}

	gs := grpc.NewServer()
	checkv1.RegisterCheckerServer(gs, server.New(cfg, slidingwindow.New(rdb)))
	reflection.Register(gs)
	log.Printf("check listening on %s (h2c)", cfg.ListenAddr)
	log.Fatal(gs.Serve(lis))
}
