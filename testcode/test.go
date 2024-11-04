package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chromedp/chromedp"
	"log"
)

type IpRiskDb struct {
	data map[string]string
}

func (db *IpRiskDb) Exists(ip string) bool {
	_, exists := db.data[ip]
	return exists
}

func (db *IpRiskDb) Get(ip string) string {
	return db.data[ip]
}

func (db *IpRiskDb) Put(ip string, val map[string]string) {
	bytes, _ := json.Marshal(val)
	db.data[ip] = string(bytes)
}

func getIpRiskScore(ip string, proxy string) (map[string]string, error) {
	ipRiskDb := &IpRiskDb{data: make(map[string]string)}

	// Check if IP exists in the database
	if ipRiskDb.Exists(ip) {
		val := ipRiskDb.Get(ip)
		var result map[string]string
		json.Unmarshal([]byte(val), &result)
		return result, nil
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
	var ip_, location, ipType, nativeIp, riskScore string

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Text(`div.line.ip > div.content`, &ip_, chromedp.NodeVisible, chromedp.ByQuery),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.loc > div.content`, &location, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.line-iptype > div.content`, &ipType, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`#check > div.container > div.info > div.content > div.line.line-nativeip > div.content > span`, &nativeIp, chromedp.NodeVisible, chromedp.ByID),
		chromedp.Text(`span.value`, &riskScore, chromedp.NodeVisible, chromedp.ByQuery),
	)

	if err != nil {
		log.Printf("Error occurred: %v", err)
		return nil, err
	}

	ret := map[string]string{
		"ip":         ip_,
		"location":   location,
		"ip_type":    ipType,
		"native_ip":  nativeIp,
		"risk_score": riskScore,
	}

	// Store result in the database
	ipRiskDb.Put(ip, ret)

	return ret, nil
}

//func main() {
//	// Example usage
//	ip := "1.1.1.1"
//	proxy := "socks5://192.168.50.88:40077"
//	result, err := getIpRiskScore(ip, proxy)
//	if err != nil {
//		log.Fatalf("Error fetching IP risk score: %v", err)
//	}
//	fmt.Println(result)
//}
