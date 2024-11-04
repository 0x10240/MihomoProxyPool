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
var listeners = make(map[string]CListener, 0)
var dbClient *db.RedisClient

type CProxy = constant.Proxy

type AddProxyReq struct {
	Link   string         `json:"link"`   // 链接
	Config map[string]any `json:"config"` // 配置，json信息
	Sub    string         `json:"sub"`    // 订阅链接
}

type Proxy struct {
	Config        map[string]any `json:"config"`
	Name          string         `json:"name"`
	LocalPort     int            `json:"local_port"`
	OutboundIp    string         `json:"ip"`
	Region        string         `json:"region"`
	IpType        string         `json:"ip_type"`
	IpRiskScore   string         `json:"ip_risk_score"`
	FailCount     int            `json:"fail"`
	SuccessCount  int            `json:"success"`
	LastCheckTime int64          `json:"last_check_time"`
	AddTime       int64          `json:"add_time"`
	Delay         int            `json:"delay"`
}

type ProxyResp struct {
	Name          string    `json:"name"`
	Server        string    `json:"server"`
	ServerPort    int       `json:"server_port"`
	AddTime       time.Time `json:"add_time"`
	LocalPort     int       `json:"local_port"`
	Success       int       `json:"success"`
	Fail          int       `json:"fail"`
	Delay         int       `json:"delay"`
	Ip            string    `json:"ip"`
	IpType        string    `json:"ip_type"`
	Region        string    `json:"region"`
	IpRiskScore   string    `json:"ip_risk_score"`
	LastCheckTime time.Time `json:"last_check_time"`
	AliveTime     string    `json:"alive_time"`
}

// CalculateAliveTime calculates the time difference in "XdXhXm" format
func CalculateAliveTime(addTime int64) string {
	duration := time.Since(ConvertTimestampToTime(addTime))

	days := duration / (24 * time.Hour)
	hours := (duration % (24 * time.Hour)) / time.Hour
	minutes := (duration % time.Hour) / time.Minute

	return fmt.Sprintf("%dd%dh%dm", days, hours, minutes)
}

func ConvertTimestampToTime(timestamp int64) time.Time {
	return time.Unix(timestamp, 0)
}

func (p Proxy) ToResp() ProxyResp {
	resp := ProxyResp{
		Name:          p.Name,
		LocalPort:     p.LocalPort,
		Server:        p.Config["server"].(string),
		ServerPort:    int(p.Config["port"].(float64)),
		Ip:            p.OutboundIp,
		IpType:        p.IpType,
		IpRiskScore:   p.IpRiskScore,
		Region:        p.Region,
		LastCheckTime: ConvertTimestampToTime(p.LastCheckTime),
		AddTime:       ConvertTimestampToTime(p.AddTime),
		Success:       p.SuccessCount,
		Fail:          p.FailCount,
		Delay:         p.Delay,
		AliveTime:     CalculateAliveTime(p.AddTime),
	}

	return resp
}

func GetProxiesFromDb() (map[string]Proxy, error) {
	resp, err := dbClient.GetAll()
	if err != nil {
		return map[string]Proxy{}, err
	}

	ret := make(map[string]Proxy, 0)

	for k, value := range resp {
		//if !strings.Contains(k, "103.114.163.93") {
		//	continue
		//}
		proxy := Proxy{}
		if err = json.Unmarshal([]byte(value), &proxy); err != nil {
			logger.Infof("unmarshal proxy: %v from db failed", value)
			continue
		}
		ret[k] = proxy
	}

	return ret, nil
}

func DeleteProxy(proxy Proxy) error {
	proxyKey := proxy.Name

	if err := dbClient.Delete(proxyKey); err != nil {
		logger.Errorf("delete proxy %s failed, err: %v", proxyKey, err)
		return err
	}

	listenerKey := getListenerKey(proxy.LocalPort)

	delete(cproxies, proxyKey)
	delete(listeners, listenerKey)
	delete(localPortMaps, proxy.LocalPort)

	tunnel.UpdateProxies(cproxies, nil)
	startListen(listeners, true)

	return nil
}

func UpdateProxyDB(proxy *Proxy) error {
	key := proxy.Config["name"].(string)
	proxy.Name = key
	proxy.LastCheckTime = time.Now().Unix()

	if err := dbClient.Put(key, proxy); err != nil {
		logger.Errorf("update proxy failed: %v", err)
		return err
	}

	return nil
}

func getListenerKey(localPort int) string {
	return fmt.Sprintf("in_%d", localPort)
}

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

		logger.Infof("%s listen at %v", proxyName, proxy.LocalPort)

		listenerKey := getListenerKey(proxy.LocalPort)
		listeners[listenerKey] = listener

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

	listenerKey := getListenerKey(localPort)
	listeners[listenerKey] = listener

	startListen(listeners, true)
	return nil
}

func GetRandomProxy() (ProxyResp, error) {
	proxy := Proxy{}
	proxyStr, err := dbClient.GetRandom()
	if err != nil {
		return ProxyResp{}, err
	}
	if err = json.Unmarshal([]byte(proxyStr), &proxy); err != nil {
		return ProxyResp{}, err
	}

	return proxy.ToResp(), nil
}

func GetAllProxies() ([]ProxyResp, error) {
	proxies, err := dbClient.GetAllValues()
	if err != nil {
		return []ProxyResp{}, err
	}

	ret := []ProxyResp{}
	for _, proxy := range proxies {
		item := Proxy{}
		if err = json.Unmarshal([]byte(proxy), &item); err != nil {
			continue
		}
		ret = append(ret, item.ToResp())
	}

	return ret, nil
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

	if req.Sub != "" {
		if err := AddSubscriptionProxies(req.Sub); err != nil {
			return err
		}
	}

	key := fmt.Sprintf("%v:%v", cfg["server"], cfg["port"])
	cfg["name"] = key
	localPort := getLocalPort()

	proxy := Proxy{
		Config:    cfg,
		AddTime:   time.Now().Unix(),
		LocalPort: localPort,
		Name:      key,
	}

	logger.Infof("Adding proxy %s on local port: %d", key, localPort)
	if err = addMihomoProxy(cfg, key, localPort); err != nil {
		return err
	}

	localPortMaps[localPort] = key
	return dbClient.Put(key, proxy)
}
