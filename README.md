

基于 [mihomo](https://github.com/MetaCubeX/mihomo 将订阅转换成本地代理池

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