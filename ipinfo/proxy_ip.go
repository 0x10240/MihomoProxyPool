package ipinfo

import (
	"crypto/tls"
	"errors"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/go-resty/resty/v2"
	logger "github.com/sirupsen/logrus"
	"math/rand"
	"net"
	"time"
)

// 检查给定的 IP 地址是否是合法的 IPv4 或 IPv6 地址
func isValidIP(ip string) bool {
	// 尝试解析 IPv4 地址
	if net.ParseIP(ip) != nil {
		return true
	}
	return false
}

func GetProxyOutboundIP2(proxy proxypool.CProxy) (string, error) {
	servers := []string{
		"http://myip1.002022.xyz:38080/",
		"http://myip2.002022.xyz:8080/",
	}
	randomServer := servers[rand.Intn(len(servers))]

	client := resty.New()
	client.SetTimeout(5 * time.Second)

	transport := proxypool.GetProxyTransport(proxy)
	client.SetTransport(transport)

	resp, err := client.R().Get(randomServer)
	if err != nil {
		return "", err
	}

	ret := resp.String()
	if !isValidIP(ret) {
		return "", errors.New("invalid ip format")
	}

	logger.Debugf("proxy: %v outbound ip is %s, status: %v", proxy.Name(), ret, resp.Status())
	return ret, nil
}

func GetProxyOutboundIP(proxy proxypool.CProxy) (string, error) {
	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	client.SetTimeout(10 * time.Second)

	transport := proxypool.GetProxyTransport(proxy)
	client.SetTransport(transport)

	resp, err := client.R().Get("https://speed.cloudflare.com/__down?bytes=1")
	if err != nil {
		return "", err
	}

	ret := resp.Header().Get("Cf-Meta-Ip")
	logger.Debugf("proxy: %v outbound ip is %s, status: %v", proxy.Name(), ret, resp.Status())
	return ret, nil
}
