package main

import (
	"flag"
	"fmt"
	"github.com/0x10240/mihomo-proxy-pool/config"
	"github.com/0x10240/mihomo-proxy-pool/healthcheck"
	"github.com/0x10240/mihomo-proxy-pool/ipinfo"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/0x10240/mihomo-proxy-pool/server"
	"github.com/sirupsen/logrus"
	"os"
	"runtime"
	"strings"
	"time"
)

// 用于存储配置路径及其他参数
var configPath string

type LogFormatter struct{}

func (c *LogFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	timestamp := time.Now().Format(time.RFC3339)

	_, file, line, ok := runtime.Caller(8)
	if !ok {
		file = "unknown"
		line = 0
	}
	fileLine := fmt.Sprintf("%s:%d", file, line)

	return []byte(fmt.Sprintf("%s [%d] %s: %s | %s\n", timestamp, os.Getpid(), strings.ToUpper(entry.Level.String()), fileLine, entry.Message)), nil
}

// parseArgs 解析命令行参数
func parseArgs() {
	flag.StringVar(&configPath, "c", "config.yaml", "Path to configuration file")

	// 可以在这里添加更多参数，例如
	// flag.BoolVar(&debugMode, "debug", false, "Enable debug mode")

	flag.Parse()

	if configPath == "" {
		fmt.Println("Error: configuration file path is required")
		os.Exit(1)
	}
}

func main() {
	// 调用 parseArgs 以解析所有参数
	parseArgs()

	// 加载配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// 设置日志格式
	logrus.SetFormatter(&LogFormatter{})

	// 初始化代理池
	if err := proxypool.InitProxyPool(cfg); err != nil {
		fmt.Printf("Error initializing proxy pool: %v\n", err)
		os.Exit(1)
	}

	if err := ipinfo.InitIpRiskDb(); err != nil {
		fmt.Printf("Error initializing ip risk db: %v\n", err)
		os.Exit(1)
	}

	// 配置服务器
	serverCfg := server.Config{
		Addr:    cfg.ServerAddr,
		IsDebug: true,
		Cors: server.Cors{
			AllowOrigins:        []string{},
			AllowPrivateNetwork: true,
		},
		Secret: cfg.Secret,
	}

	// 启动健康检查调度器
	go healthcheck.StartHealthCheckScheduler()

	// 启动服务器
	if err := server.Start(&serverCfg); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}
