package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/0x10240/mihomo-proxy-pool/config"
	"github.com/0x10240/mihomo-proxy-pool/db"
	"github.com/chromedp/chromedp"
	logger "github.com/sirupsen/logrus"
	"log"
	"strings"
)

var ipRiskDb *db.RedisClient

func init() {
	redisConn := config.GetRedisConn()
	ipRiskDb, _ = db.NewRedisClientFromURL("ip_risk", redisConn)
}

type IpRiskScore struct {
	Ip        string `json:"ip"`
	Location  string `json:"location"`
	IpType    string `json:"ip_type"`
	NativeIp  string `json:"native_ip"`
	RiskScore string `json:"risk_score"`
}

func GetIpRiskScore(server string, proxy string) (IpRiskScore, error) {
	result := IpRiskScore{}

	val, _ := ipRiskDb.Get(server)
	if val != "" {
		if err := json.Unmarshal([]byte(val), &result); err == nil {
			return result, nil
		}
	}

	// Set up context with custom allocator options
	opts := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"),
	}
	if proxy != "" {
		opts = append(opts, chromedp.ProxyServer(proxy))
	}
	allocatorCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()

	// URL to navigate
	url := fmt.Sprintf("https://ping0.cc/")
	if server != "" {
		url = fmt.Sprintf("https://ping0.cc/ip/%s", server)
	}

	var ip, location, ipType, nativeIp, riskScore string

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Text(`div.line.ip > div.content`, &ip, chromedp.NodeVisible, chromedp.ByQuery),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.loc > div.content`, &location, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.line-iptype > div.content`, &ipType, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.line-nativeip > div.content > span`, &nativeIp, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`span.value`, &riskScore, chromedp.NodeVisible, chromedp.ByQuery),
	)

	if err != nil {
		log.Printf("Error occurred: %v", err)
		return result, err
	}

	result.Ip = strings.Fields(ip)[0]
	result.Location = location
	result.IpType = ipType
	result.NativeIp = nativeIp
	result.RiskScore = riskScore

	// Store result in the database
	if err = ipRiskDb.Put(ip, result); err != nil {
		logger.Warning("put ip: %v score to db failed, err: %v", server, err)
	}

	return result, nil
}
