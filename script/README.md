

基于 [mihomo](https://github.com/MetaCubeX/mihomo 将订阅转换成本地代理池

## 使用说明

1. 安装 docker
```bash
curl -fsSL https://get.docker.com -o get-docker.sh && sudo sh get-docker.sh
```

2. 拉取 `mihomo` 镜像
```bash
docker pull metacubex/mihomo
```

3. 安装依赖
```bash
pip install -r requirements.txt
```

4. 运行 `main.py`
```bash
python main.py
```