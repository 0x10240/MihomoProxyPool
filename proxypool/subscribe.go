package proxypool

import (
	"context"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/metacubex/mihomo/constant"
	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"math/rand"
	"net"
	"net/http"
	"strconv"
)

type RawConfig struct {
	Providers map[string]map[string]any `yaml:"proxy-providers"`
	Proxies   []map[string]any          `yaml:"proxies"`
}

func getProxyTransport(proxy CProxy) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portStr, 10, 16)
			if err != nil {
				return nil, err
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: uint16(port),
			})
		},
	}
}

func getRandomCProxy() CProxy {
	// 获取字典的键
	keys := make([]string, 0, len(cproxies))
	for k := range cproxies {
		keys = append(keys, k)
	}
	// 随机选择一个键
	randomKey := keys[rand.Intn(len(keys))]
	return cproxies[randomKey]
}

func readConfig(url string, proxy CProxy) ([]byte, error) {
	// 创建 Resty 客户端
	client := resty.New()

	// 如果 proxy 不为空，设置代理
	if proxy != nil {
		transPort := getProxyTransport(proxy)
		client.SetTransport(transPort)
	}

	// 发起 GET 请求
	resp, err := client.R().
		SetHeader("User-Agent", "clash.meta").
		Get(url)

	if err != nil {
		return nil, fmt.Errorf("HTTP GET failed: %v", err)
	}

	// 返回响应体
	return resp.Body(), nil
}

func AddSubscriptionProxies(url string) error {
	var cproxy CProxy
	for i := 0; i < 5; i++ {
		cproxy = getRandomCProxy()
		if cproxy.AliveForTestUrl(url) {
			break
		}
	}

	body, err := readConfig(url, cproxy)
	if err != nil {
		return err
	}

	rawCfg := &RawConfig{}
	if err = yaml.Unmarshal(body, rawCfg); err != nil {
		return fmt.Errorf("YAML unmarshal failed: %v", err)
	}

	for _, rawProxy := range rawCfg.Proxies {
		if err = addProxyIfNotExists(rawProxy); err != nil {
			logger.Errorf("AddProxy failed for %v: %v", rawProxy, err)
		}
	}

	for _, provider := range rawCfg.Providers {
		if providerUrl, ok := provider["url"].(string); ok {
			if err := AddSubscriptionProxies(providerUrl); err != nil {
				logger.Errorf("Failed to add provider %v proxies: %v", providerUrl, err)
			}
		}
	}

	return nil
}

func addProxyIfNotExists(rawProxy map[string]any) error {
	key := fmt.Sprintf("%v:%v", rawProxy["server"], rawProxy["port"])
	if dbClient.Exists(key) {
		logger.Infof("Proxy key: %s already exists", key)
		return nil
	}

	return AddProxy(AddProxyReq{Config: rawProxy})
}
