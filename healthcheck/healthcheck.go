package healthcheck

import (
	"fmt"
	"sync"
	"time"

	"github.com/0x10240/mihomo-proxy-pool/ipinfo"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/metacubex/mihomo/adapter"
	logger "github.com/sirupsen/logrus"
)

const (
	concurrency   = 16 // 并发限制
	maxFailCount  = 6
	checkInterval = 10 * time.Minute
)

// DoHealthCheck 对代理池中的所有代理进行健康检查
func DoHealthCheck() error {
	proxies, err := proxypool.GetProxiesFromDb()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency) // 并发限制

	for _, p := range proxies {
		proxy := p // 创建代理副本，避免闭包问题
		wg.Add(1)
		go func(proxy proxypool.Proxy) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取一个令牌
			defer func() { <-semaphore }() // 确保令牌释放

			processProxyHealthCheck(proxy)
		}(proxy)
	}

	wg.Wait()
	return nil
}

// processProxyHealthCheck 执行单个代理的健康检查
func processProxyHealthCheck(proxy proxypool.Proxy) {
	cproxy, err := adapter.ParseProxy(proxy.Config)
	if err != nil {
		logger.Errorf("parse proxy %v failed: err: %v", proxy.Name, err)
		return
	}

	// 在每个 goroutine 中定义 err 为局部变量，避免数据竞争
	delay, err := proxypool.CheckProxy(cproxy)
	logger.Debugf("proxy %v: delay: %v", proxy.Name, delay)
	if err != nil {
		logger.Infof("check proxy: %s, err: %v", proxy.Name, err)
		proxy.FailCount++
		if proxy.FailCount >= maxFailCount { // 使用 maxFailCount 常量
			logger.Infof("delete proxy: %v", proxy.Name)
			if delErr := proxypool.DeleteProxy(proxy); delErr != nil {
				logger.Errorf("delete proxy: %s, err: %v", proxy.Name, delErr)
			}
			return // 删除后返回，防止进入更新数据库的逻辑
		}
	} else {
		proxy.SuccessCount++
		proxy.FailCount = 0
	}

	proxy.Delay = delay
	if err == nil && proxy.OutboundIp == "" {
		proxy.OutboundIp = ipinfo.GetProxyOutboundIP(cproxy)
	}

	if err == nil && proxy.IpRiskScore == "" {
		localPort := proxypool.GetRandomLocalPort()
		proxyStr := fmt.Sprintf("http://127.0.0.1:%d", localPort)
		ipRiskVal, err := ipinfo.GetIpRiskScore(proxy.OutboundIp, proxyStr)
		if err != nil {
			logger.Infof("get ip risk info failed via %s", proxyStr)
			return
		}
		proxy.IpType = ipRiskVal.IpType
		proxy.IpRiskScore = ipRiskVal.RiskScore
		proxy.Region = ipRiskVal.Location
	}

	// 只有当代理未被删除时，才更新数据库
	if updateErr := proxypool.UpdateProxyDB(&proxy); updateErr != nil {
		logger.Errorf("update proxy: %+v, err: %v", proxy, updateErr)
	}
}

// StartHealthCheckScheduler 每10分钟执行一次DoHealthCheck
func StartHealthCheckScheduler() {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		err := DoHealthCheck()
		if err != nil {
			logger.Errorf("DoHealthCheck error: %v", err)
		}
		<-ticker.C // 等待下一个周期
	}
}
