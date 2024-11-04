package main

import (
	"github.com/0x10240/mihomo-proxy-pool/healthcheck"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/0x10240/mihomo-proxy-pool/server"
	"os"
)

func main() {
	if err := proxypool.InitProxyPool(); err != nil {
		os.Exit(1)
	}

	cfg := server.Config{
		Addr:    "0.0.0.0:9999",
		IsDebug: true,
		Cors: server.Cors{
			AllowOrigins:        []string{},
			AllowPrivateNetwork: true,
		},
	}

	go healthcheck.StartHealthCheckScheduler()

	server.Start(&cfg)
}
