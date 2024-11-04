package proxypool

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"math/rand"
)

type RawConfig struct {
	Providers map[string]map[string]any `yaml:"proxy-providers"`
	Proxies   []map[string]any          `yaml:"proxies"`
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
		transPort := GetProxyTransport(proxy)
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

func AddSubscriptionProxies(req AddProxyReq) error {
	url := req.SubUrl

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
		err := AddProxy(AddProxyReq{
			Config:      rawProxy,
			SubName:     req.SubName,
			ForceUpdate: req.ForceUpdate,
		})
		if err != nil {
			logger.Errorf("AddProxy failed for %v: %v", rawProxy, err)
		}
	}

	for providerName, provider := range rawCfg.Providers {
		if providerUrl, ok := provider["url"].(string); ok {
			err := AddSubscriptionProxies(AddProxyReq{
				SubUrl:      providerUrl,
				SubName:     providerName,
				ForceUpdate: req.ForceUpdate,
			})
			if err != nil {
				logger.Errorf("Failed to add provider %v proxies: %v", providerUrl, err)
			}
		}
	}

	return nil
}
