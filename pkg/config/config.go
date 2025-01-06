package config

import (
	"gopkg.in/yaml.v2"
	"os"
)

const (
	NameSpace = "douban"
)

type CustomQueryDimension struct {
	ProjectID int    `yaml:"projectId"`
	Domain    string `yaml:"domain"`
}

type TencentConfig struct {
	Metrics              []string               `yaml:"metrics"`
	RateLimit            int                    `yaml:"rate_limit"`
	DelaySeconds         int                    `yaml:"delay_seconds"`
	CustomQueryDimension []CustomQueryDimension `yaml:"custom_query_dimensions"`
}

func (c *TencentConfig) LoadFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, c)
	if err != nil {
		return err
	}

	return nil
}
