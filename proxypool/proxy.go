package proxypool

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0x10240/mihomo-proxy-pool/db"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
	logger "github.com/sirupsen/logrus"
	"math/rand"
	"net/netip"
	"time"
)

var proxyPoolStartPort = 40001
var allowIps = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
var localPortMaps = make(map[int]string, 0)
var cproxies = make(map[string]CProxy, 0)

type CProxy = constant.Proxy

type AddProxyReq struct {
	Link   string         `json:"link"`
	Config map[string]any `json:"config"`
}

type Proxy struct {
	Config        map[string]any `json:"config"`
	LocalPort     int            `json:"local_port"`
	OutboundIp    string         `json:"ip"`
	Region        string         `json:"region"`
	IpRiskScore   int            `json:"ip_risk_score"`
	FailCount     int            `json:"fail"`
	SuccessCount  int            `json:"success"`
	LastCheckTime int            `json:"last_check_time"`
	AddTime       int64          `json:"add_time"`
}

var dbClient *db.RedisClient

func InitProxyPool() error {
	var err error
	dbClient, err = db.NewRedisClientFromURL("mihomo_proxy_pool", "redis://:@192.168.50.88:6379/0")
	if err != nil {
		return err
	}

	values, err := dbClient.GetAllValues()
	if err != nil {
		return err
	}

	listeners := make(map[string]CListener, 0)

	for _, value := range values {
		proxy := Proxy{}

		if err = json.Unmarshal([]byte(value), &proxy); err != nil {
			continue
		}

		cproxy, err := adapter.ParseProxy(proxy.Config)
		if err != nil {
			continue
		}

		proxyName := cproxy.Name()
		cproxies[proxyName] = cproxy

		listener, err := getListenerByLocalPort(proxy.LocalPort, proxyName)
		if err != nil {
			continue
		}

		logger.Infof("%s listen as %v", proxyName, proxy.LocalPort)

		listeners[fmt.Sprintf("in_%d", proxy.LocalPort)] = listener

		localPortMaps[proxy.LocalPort] = proxyName
	}

	inbound.SetAllowedIPs(allowIps)
	tunnel.UpdateProxies(cproxies, nil)
	startListen(listeners, true)
	tunnel.OnRunning()

	return nil
}

func parseProxyLink(link string) (map[string]any, error) {
	ret := map[string]any{}

	cfgs, err := convert.ConvertsV2Ray([]byte(link))
	if err != nil {
		return ret, err
	}

	if len(cfgs) != 1 {
		return ret, errors.New("invalid proxy link")
	}

	return ret, nil
}

func getLocalPort() int {
	for p := proxyPoolStartPort; p <= 65535; p++ {
		if _, ok := localPortMaps[p]; !ok {
			return p
		}
	}
	return rand.Intn(65535)
}

func addMihomoProxy(proxyCfg map[string]any, proxyName string, localPort int) error {
	cproxy, err := adapter.ParseProxy(proxyCfg)
	if err != nil {
		return err
	}

	cproxies[proxyName] = cproxy
	tunnel.UpdateProxies(cproxies, nil)

	listener, err := getListenerByLocalPort(localPort, proxyName)
	if err != nil {
		return err
	}

	listeners := map[string]CListener{
		fmt.Sprintf("in_%d", localPort): listener,
	}

	startListen(listeners, false)
	return nil
}

func AddProxy(req AddProxyReq) error {
	var cfg map[string]any
	var err error

	if req.Link != "" {
		if cfg, err = parseProxyLink(req.Link); err != nil {
			return err
		}
	}

	if len(req.Config) > 0 {
		cfg = req.Config
	}

	key := fmt.Sprintf("%s:%d", cfg["server"].(string), int(cfg["port"].(float64)))
	if dbClient.Exists(key) {
		logger.Infof("key: %s exists", key)
		return nil
	}

	cfg["name"] = key
	localPort := getLocalPort()

	proxy := Proxy{
		Config:    cfg,
		AddTime:   time.Now().Unix(),
		LocalPort: localPort,
	}

	logger.Infof("add proxy %s, local port: %d", key, localPort)

	localPortMaps[localPort] = key

	if err = addMihomoProxy(cfg, key, localPort); err != nil {
		return err
	}

	if err = dbClient.Put(key, proxy); err != nil {
		return nil
	}

	return nil
}

func GetRandomProxy() (Proxy, error) {
	ret := Proxy{}

	proxy, err := dbClient.GetRandom()
	if err != nil {
		return ret, err
	}
	if err = json.Unmarshal([]byte(proxy), &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

func GetAllProxies() ([]Proxy, error) {
	proxies, err := dbClient.GetAllValues()
	if err != nil {
		return []Proxy{}, err
	}

	ret := []Proxy{}
	for _, proxy := range proxies {
		item := Proxy{}
		if err = json.Unmarshal([]byte(proxy), &item); err != nil {
			continue
		}
		ret = append(ret, item)
	}

	return ret, nil
}
