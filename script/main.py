import os
import yaml
import sys
import requests
import argparse
import subprocess
from dotenv import load_dotenv

load_dotenv()


# 定义一个自定义的字典类，用于流式显示
class FlowDict(dict):
    pass


# 定义自定义的表示器，使 FlowDict 以流式风格显示
def flow_dict_representer(dumper, value):
    return dumper.represent_mapping('tag:yaml.org,2002:map', value, flow_style=True)


# 定义自定义的 Dumper
class CustomDumper(yaml.Dumper):
    pass


# 将表示器添加到 Dumper
yaml.add_representer(FlowDict, flow_dict_representer, Dumper=CustomDumper)

mihomo_config_base = {
    'allow-lan': True,
    'listeners': [],
    'proxies': []
}

current_dir = os.path.abspath(os.path.dirname(__file__))


class MiHoMoProxyPool:
    def __init__(self, sub_url='', clash_cfg_path=''):

        self.sub_url = sub_url
        self.clash_cfg_path = clash_cfg_path
        self.headers = {"User-Agent": "Clash.Meta; Mihomo"}

        self.mihomo_config = mihomo_config_base.copy()
        self.mihomo_config_dir = os.path.join(current_dir, 'configs')
        self.docker_compose_file_path = os.path.join(current_dir, 'docker-compose.yml')

        self.proxies = self.load_proxies()
        self.authentication = self.load_authentication()

    def load_authentication(self):
        user = os.getenv('AUTH_USER', None)
        password = os.getenv('AUTH_PASSWORD', None)
        if user and password:
            self.mihomo_config['authentication'] = [f'{user}:{password}']

    def load_proxies(self):
        if self.clash_cfg_path:
            data = yaml.full_load(self.clash_cfg_path)
            return data['proxies']

        if self.sub_url:
            req = requests.get(self.sub_url, headers=self.headers)
            req.raise_for_status()
            req.encoding = 'utf-8'
            proxies = yaml.full_load(req.text)['proxies']
            return proxies

        raise Exception('not provide sub url or clash cfg path')

    def pre_process_proxies(self):
        new_proxies = []
        name_set = set()

        def get_new_name(name, name_set):
            i = 1
            while name in name_set:
                name = f'{name}-{i}'
                i += 1
            return name

        for proxy in self.proxies:
            # 最新 mihomo 只支持 xtls-rprx-vision 流控算法
            if proxy.get('type') == 'vless' and proxy.get('flow') and proxy.get('flow'):
                continue

            # 转换器的问题，chacha20-poly1305 在 mihomo 要写成 chacha20-ietf-poly1305
            if proxy.get('type') == 'ss' and 'poly1305' in proxy.get('cipher'):
                proxy['cipher'] = 'chacha20-ietf-poly1305'

            fail_count = proxy.get('fail_count', 0)
            if fail_count > 0:
                print(f'proxy: {proxy} fail count: {fail_count}, skip...')
                continue

            p = proxy.copy()
            new_name = get_new_name(p['name'], name_set)
            p['name'] = new_name
            name_set.add(new_name)
            new_proxies.append(p)

        self.proxies = new_proxies
        self.proxy_dict = {f"{x['server']}:{x['port']}": x for x in self.proxies}
        return new_proxies

    def generate_mihomo_config(self):
        proxies = self.pre_process_proxies()
        total_proxies = len(proxies)
        print(f'total_proxies: {total_proxies}')

        config = self.mihomo_config
        listeners = []

        start_port = int(os.getenv('PROXY_POOL_START_PORT', 42001))
        for i in range(len(proxies)):
            proxy = proxies[i]
            local_port = start_port + i
            listeners.append({
                'name': f"mixed{local_port}",
                'type': 'mixed',
                'port': local_port,
                'proxy': proxy['name']
            })

        config['listeners'] = [FlowDict(x) for x in listeners]
        config['proxies'] = [FlowDict(x) for x in proxies]

        config_path = os.path.join(self.mihomo_config_dir, 'mihomo.yaml')
        with open(config_path, 'w', encoding='utf-8') as f:
            yaml.dump(config, f, Dumper=CustomDumper, allow_unicode=True, width=1024)

        return config_path

    def generate_docker_compose_config(self):
        mihomo_config_path = self.generate_mihomo_config()
        docker_compose_dict = {
            'services': {
                'mihomo': {
                    'container_name': 'mihomo',
                    'build': '.',
                    'restart': 'always',
                    'network_mode': "host",
                    'volumes': [
                        {
                            'type': "bind",
                            'bind': {'propagation': "rprivate"},
                            'source': mihomo_config_path,
                            'target': '/etc/mihomo/config.yaml'
                        }
                    ],
                    'image': 'metacubex/mihomo',
                    'command': '-f /etc/mihomo/config.yaml'
                }
            }
        }

        with open(os.path.join(current_dir, self.docker_compose_file_path), 'w', encoding='utf-8') as file:
            yaml.dump(docker_compose_dict, file, default_flow_style=False, sort_keys=False)

    def stop_mihomo_docker(self):
        stop_cmd = f"docker compose -f {self.docker_compose_file_path} down"
        stop_ret = subprocess.getoutput(stop_cmd)
        print(f'stop cmd: {stop_ret} ret: {stop_ret}')

    def start_mihomo_docker(self):
        start_cmd = f"docker compose -f {self.docker_compose_file_path} up -d --remove-orphans"
        start_ret = subprocess.getoutput(start_cmd)
        print(f'start cmd:{start_cmd} ret: {start_ret}')

    def run(self):
        self.stop_mihomo_docker()
        self.generate_docker_compose_config()
        self.start_mihomo_docker()


def main():
    # 创建 ArgumentParser 对象
    parser = argparse.ArgumentParser(description="Handle subscription link or config file")

    # 添加 -u 和 -f 参数
    parser.add_argument('-u', '--url', type=str, help='Subscription URL')
    parser.add_argument('-f', '--file', type=str, help='Path to config file')

    # 解析参数
    args = parser.parse_args()

    # 检查是否至少提供了 -u 或 -f
    if not args.url and not args.file:
        print("Error: You must provide either a subscription URL (-u) or a config file (-f).")
        parser.print_help()
        sys.exit(1)

    m = MiHoMoProxyPool(args.url, args.file)
    m.run()


if __name__ == "__main__":
    main()
