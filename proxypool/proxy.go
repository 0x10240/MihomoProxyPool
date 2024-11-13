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
	cutils "github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
	logger "github.com/sirupsen/logrus"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const defaultTimeout = 10 * time.Second

var allowIps = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}

var localPortMap = sync.Map{}
var cproxies = make(map[string]CProxy, 0)
var listeners = make(map[string]CListener, 0)

var dbClient *db.RedisClient
var mu = sync.Mutex{}
var portMu = sync.Mutex{}

type CProxy = constant.Proxy

type AddProxyReq struct {
	Link        string         `json:"link"`   // 链接
	Config      map[string]any `json:"config"` // 配置，json信息
	SubUrl      string         `json:"sub"`    // 订阅链接
	SubName     string         `json:"sub_name"`
	ForceUpdate bool           `json:"update"`
}

type AddProxyResp struct {
	Success int32 `json:"success"`
	Failure int32 `json:"failure"`
	Exist   int32 `json:"exist"`
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
	Name          string         `json:"name"`
	Server        string         `json:"server"`
	ServerPort    int            `json:"server_port"`
	AddTime       time.Time      `json:"add_time"`
	LocalPort     int            `json:"local_port"`
	Success       int            `json:"success"`
	Fail          int            `json:"fail"`
	Delay         int            `json:"delay"`
	Ip            string         `json:"ip"`
	IpType        string         `json:"ip_type"`
	Region        string         `json:"region"`
	IpRiskScore   string         `json:"ip_risk_score"`
	LastCheckTime time.Time      `json:"last_check_time"`
	AliveTime     string         `json:"alive_time"`
	SubName       string         `json:"sub,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
}

func (r *AddProxyResp) IncrementSuccess() {
	atomic.AddInt32(&r.Success, 1)
}

func (r *AddProxyResp) IncrementFailure() {
	atomic.AddInt32(&r.Failure, 1)
}

func (r *AddProxyResp) IncremenExist() {
	atomic.AddInt32(&r.Exist, 1)
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

func (p Proxy) ToResp(showConfig bool) ProxyResp {
	var serverPort int
	if port, ok := p.Config["port"].(float64); ok {
		serverPort = int(port)
	} else if portStr, ok := p.Config["port"].(string); ok {
		// 如果是字符串类型，尝试将其转换为整数
		portInt, err := strconv.Atoi(portStr)
		if err == nil {
			serverPort = portInt
		} else {
			// 处理无法转换为整数的情况，比如给个默认值
			serverPort = 0 // 或者适当的默认值
		}
	} else {
		// 处理无法识别的类型，比如给个默认值
		serverPort = 0 // 或者适当的默认值
	}

	resp := ProxyResp{
		Name:          p.Name,
		LocalPort:     p.LocalPort,
		Server:        p.Config["server"].(string),
		ServerPort:    serverPort,
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

	if showConfig {
		resp.Config = p.Config
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

	listenerKey := proxyKey

	delete(cproxies, proxyKey)
	delete(listeners, listenerKey)
	localPortMap.Delete(proxy.LocalPort)

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

		listenerKey := proxyName
		listeners[listenerKey] = listener

		localPortMap.Store(proxy.LocalPort, proxyName)
	}

	inbound.SetAllowedIPs(allowIps)
	tunnel.UpdateProxies(cproxies, nil)
	startListen(listeners, true)
	tunnel.OnRunning()

	return nil
}

func parseProxyLink(link string) (map[string]any, error) {
	ret := map[string]any{}

	encodedLink := enc.EncodeToString([]byte(link))
	cfgs, err := convert.ConvertsV2Ray([]byte(encodedLink))
	if err != nil {
		return ret, err
	}

	if len(cfgs) != 1 {
		return ret, errors.New("invalid proxy link")
	}

	for _, cfg := range cfgs {
		return cfg, nil
	}

	return ret, errors.New("parse proxy link failed")
}

// 检查端口是否被操作系统占用
func isPortAvailable(port int) bool {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	// 尝试使用 Dial 而不是 Listen
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		// 端口未被使用
		return true
	}
	// 关闭连接，因为端口已经被占用
	_ = conn.Close()
	return false
}

// 获取可用端口
func getLocalPort(val string) int {
	proxyPoolStartPort := config.GetPoolStartPort()

	portMu.Lock()
	defer portMu.Unlock()

	for port := proxyPoolStartPort; port <= 65535; port++ {
		if _, ok := localPortMap.Load(port); !ok && isPortAvailable(port) {
			localPortMap.Store(port, val)
			return port
		}
	}

	// 如果未找到端口，随机选择一个
	for {
		port := rand.Intn(65535-1024) + 1024 // 避免使用低于1024的端口
		if isPortAvailable(port) {
			localPortMap.Store(port, val)
			return port
		}
	}
}

func addMihomoProxy(proxyCfg map[string]any, proxyName string, localPort int) error {
	cproxy, err := adapter.ParseProxy(proxyCfg)
	if err != nil {
		return err
	}

	delay, err := CheckProxy(cproxy)
	if err != nil {
		logger.Infof("check proxy： %s failed: %v", proxyName, err)
		return err
	}
	logger.Debugf("add mihomo proxy %s to proxy pool, delay: %v", proxyName, delay)

	// 加锁防止并发出错
	mu.Lock()
	defer mu.Unlock()

	cproxies[proxyName] = cproxy
	tunnel.UpdateProxies(cproxies, nil)

	listener, err := getListenerByLocalPort(localPort, proxyName)
	if err != nil {
		return err
	}

	listeners[proxyName] = listener

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

	return proxy.ToResp(false), nil
}

func GetAllProxies(showConfig bool) ([]ProxyResp, error) {
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
		ret = append(ret, item.ToResp(showConfig))
	}

	return ret, nil
}

func AddProxy(req AddProxyReq, resp *AddProxyResp) error {
	var cfg map[string]any
	var err error

	if req.Link != "" {
		if cfg, err = parseProxyLink(req.Link); err != nil {
			resp.IncrementFailure()
			return err
		}
	}

	if len(req.Config) > 0 {
		cfg = req.Config
	}

	if port, ok := cfg["port"].(string); ok {
		// 将字符串转换为整数
		portInt, err := strconv.Atoi(port)
		if err != nil {
			resp.IncrementFailure()
			return fmt.Errorf("invalid port value: %v", err)
		}
		cfg["port"] = portInt
	}

	key := fmt.Sprintf("%v:%v", cfg["server"], cfg["port"])
	if !req.ForceUpdate && dbClient.Exists(key) {
		logger.Infof("key: %s exists", key)
		resp.IncremenExist()
		return nil
	}

	cfg["name"] = key
	localPort := getLocalPort(key)
	proxy := Proxy{
		Config:    cfg,
		AddTime:   time.Now().Unix(),
		LocalPort: localPort,
		Name:      key,
		SubName:   req.SubName,
	}

	logger.Infof("Adding proxy %s on local port: %d", key, localPort)

	if err = addMihomoProxy(cfg, key, localPort); err != nil {
		localPortMap.Delete(localPort)
		resp.IncrementFailure()
		return err
	}

	resp.IncrementSuccess()
	return dbClient.Put(key, proxy)
}

func GetRandomLocalPort() int {
	var randomPort int
	count := 0

	localPortMap.Range(func(key, value any) bool {
		port, ok := key.(int)
		if !ok {
			return true // 如果类型断言失败，继续遍历
		}
		// Reservoir Sampling（蓄水池抽样）
		if count == 0 || rand.Intn(count+1) == 0 {
			randomPort = port
		}
		count++
		return true // 继续遍历
	})

	return randomPort
}

// checkProxy 检查单个代理的健康状况
func CheckProxy(cproxy CProxy) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	expectedStatus, _ := cutils.NewUnsignedRanges[uint16]("200-300")

	testUrl := config.GetDelayTestUrl()
	delay, err := cproxy.URLTest(ctx, testUrl, expectedStatus)
	return int(delay), err
}

func GetLocalPortMap() []map[int]string {
	arr := make([]map[int]string, 0)
	localPortMap.Range(func(key, value any) bool {
		arr = append(arr, map[int]string{key.(int): value.(string)})
		return true
	})

	// 按照键值（端口号）升序排序
	sort.Slice(arr, func(i, j int) bool {
		// 获取每个 map 的第一个键，因为 map 中只存一个键值对
		var keyI, keyJ int
		for k := range arr[i] {
			keyI = k
		}
		for k := range arr[j] {
			keyJ = k
		}
		return keyI < keyJ
	})

	return arr
}
