package config

import (
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	RedisConn          string `yaml:"redis_conn"`
	DelayTestUrl       string `yaml:"delay_test_url"`
	ServerAddr         string `yaml:"server_addr"`
	ProxyPoolStartPort int    `yaml:"proxy_pool_start_port"`
	SoftDelete         bool   `yaml:"soft_delete"`
	AuthUser           string `yaml:"auth_user"`
	AuthPass           string `yaml:"auth_pass"`
	Secret             string `yaml:"secret"`
}

var config Config

// LoadConfig loads the configuration from the given YAML file path
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func GetDelayTestUrl() string {
	return config.DelayTestUrl
}

func GetRedisConn() string {
	return config.RedisConn
}

func GetPoolStartPort() int {
	return config.ProxyPoolStartPort
}

func IsSoftDelete() bool {
	return config.SoftDelete
}
