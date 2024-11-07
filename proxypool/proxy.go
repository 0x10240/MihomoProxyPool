package proxypool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0x10240/mihomo-proxy-pool/config"
	"github.com/0x10240/mihomo-proxy-pool/db"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
	logger "github.com/sirupsen/logrus"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

type CProxy = constant.Proxy

var allowIps = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
var localPortMaps = make(map[int]string, 0)
var cproxies = make(map[string]CProxy, 0)
var listeners = make(map[string]CListener, 0)
var proxyPoolStartPort = config.GetPoolStartPort()

var dbClient *db.RedisClient
var mu = sync.Mutex{}

type AddProxyReq struct {
	Link        string         `json:"link"`   // 链接
	Config      map[string]any `json:"config"` // 配置，json信息
	SubUrl      string         `json:"sub"`    // 订阅链接
	SubName     string         `json:"sub_name"`
	ForceUpdate bool           `json:"update"`
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
	SubName       string         `json:"sub"`
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
	SubName       string    `json:"sub"`
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
		AliveTime:     CalculateAliveTime(p.AddTime),
		Success:       p.SuccessCount,
		Fail:          p.FailCount,
		Delay:         p.Delay,
		SubName:       p.SubName,
	}

	return resp
}

func GetProxyTransport(proxy CProxy) *http.Transport {
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
	mu.Lock()
	defer mu.Unlock()

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
	mu.Lock()
	defer mu.Unlock()

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
	redisConn := config.GetRedisConn()
	dbClient, err = db.NewRedisClientFromURL("mihomo_proxy_pool", redisConn)
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

	key := fmt.Sprintf("%v:%v", cfg["server"], cfg["port"])
	if !req.ForceUpdate && dbClient.Exists(key) {
		logger.Infof("key: %s exists", key)
		return nil
	}

	cfg["name"] = key
	localPort := getLocalPort()
	proxy := Proxy{
		Config:    cfg,
		AddTime:   time.Now().Unix(),
		LocalPort: localPort,
		Name:      key,
		SubName:   req.SubName,
	}

	logger.Infof("Adding proxy %s on local port: %d", key, localPort)
	if err = addMihomoProxy(cfg, key, localPort); err != nil {
		return err
	}

	localPortMaps[localPort] = key
	return dbClient.Put(key, proxy)
}

func GetRandomLocalPort() int {
	var randomPort int
	count := 0
	for k := range localPortMaps {
		// Reservoir Sampling（蓄水池抽样）
		// 如果 count 为 0 或者随机数为 0，则选择当前的键
		if count == 0 || rand.Intn(count+1) == 0 {
			randomPort = k
		}
		count++
	}
	return randomPort
}
