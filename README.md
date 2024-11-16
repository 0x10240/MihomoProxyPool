

## 介绍

`MihomoProxyPool` 是基于 [mihomo](https://github.com/MetaCubeX/mihomo) (clash-meta) 内核, 将订阅节点转换为本地 `sock5/http` 代理池的项目

支持 `Shadowsocks`, `ShadowsocksR`, `Vmess`, `Vless`, `Trojan`, `Hysteria`, `Hysteria2`, `WireGuard`, `Socks5`, `Http` 等协议

比免费代理池更加安全、稳定、快速(免费代理没有加密, 风险值高, ip万人骑), 比付费代理更实惠(机场的订阅几块钱就有几十个节点, 而付费代理1G流量就要$1)

支持通过接口动态添加/删除代理

## 使用说明

1. 安装 docker

```bash
curl -fsSL https://get.docker.com -o get-docker.sh && sudo sh get-docker.sh
```

2. 安装 redis 数据库

```bash
docker run -d \
  --name redis \
  -p 6379:6379 \
  -v $(pwd)/redis_data:/data \
  --restart always \
  redis
```

3. 编译本项目

```bash
go build . -o mihomo-proxy-pool
```

4. 运行

```bash
./mihomo-proxy-pool
```

## API 接口

### 添加代理

#### 通过订阅链接批量添加

POST http://127.0.0.1:9999/add

data
```json
{
	"sub_name": "xxx",
	"sub": "https://xxx/api/v1/client/subscribe?token=token"
}
```

#### 通过clash配置添加：

POST http://127.0.0.1:9999/add

data：
```json
{
  "config": {
    "name": "xxx:1080",
    "type": "ss",
    "server": "xxx",
    "port": 1116,
    "cipher": "aes-256-gcm",
    "password": "password",
    "udp": true,
    "udp_over_tcp": false,
    "plugin": "",
    "plugin_opts": {}
  }
}
```

#### 获取所有代理

GET http://127.0.0.1:9999/all?sort=risk_score

返回示例：
```json
{
  "count": 999,
  "proxies": [
    {
      "name": "xxx:1116",
      "server": "xxx",
      "server_port": 1116,
      "add_time": "2024-11-04T09:52:45+08:00",
      "local_port": 40360,
      "success": 460,
      "fail": 0,
      "delay": 2157,
      "ip": "xxx",
      "ip_type": "家庭宽带IP",
      "region": "美国 加州 洛杉矶",
      "ip_risk_score": "10%",
      "last_check_time": "2024-11-07T14:12:06+08:00",
      "alive_time": "3d4h28m",
      "sub": ""
    }
  ]
}
```

#### 随机获取1个代理

GET http://127.0.0.1:9999/get

```json
{
	"name": "xxx:22222",
	"server": "xxx",
	"server_port": 22222,
	"add_time": "2024-11-06T17:07:23+08:00",
	"local_port": 40594,
	"success": 130,
	"fail": 0,
	"delay": 436,
	"ip": "xxx",
	"ip_type": "IDC机房IP",
	"region": "美国 加州 洛杉矶",
	"ip_risk_score": "22%",
	"last_check_time": "2024-11-07T14:12:18+08:00",
	"alive_time": "0d21h12m",
	"sub": ""
}
```