package ipinfo

import (
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/go-resty/resty/v2"
	"time"
)

func GetProxyOutboundIP(proxy proxypool.CProxy) string {
	timeout := 5 * time.Second
	client := resty.New()
	transport := proxypool.GetProxyTransport(proxy)
	client.SetTimeout(timeout)
	client.SetTransport(transport)
	resp, err := client.R().Get("https://speed.cloudflare.com/__down?bytes=1")
	if err != nil {
		return ""
	}
	return resp.Header().Get("Cf-Meta-Ip")
}
