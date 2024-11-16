FROM dockerpull.org/debian:12-slim

ENV TIME_ZONE=Asia/Shanghai
ENV TZ=Asia/Shanghai

# 安装 CA 证书
RUN apt-get update && apt-get install -y ca-certificates

# 从构建阶段复制已编译的二进制文件到运行镜像
COPY ./mihomo-proxy-pool /app/mihomo-proxy-pool

RUN chmod 755 /app/mihomo-proxy-pool

# 暴露应用运行的端口
EXPOSE 9999

# 启动服务
CMD ["/app/mihomo-proxy-pool"]
