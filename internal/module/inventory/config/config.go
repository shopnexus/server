package inventoryconfig

import "shopnexus-server/config"

type Config struct {
	Postgres config.Postgres `mapstructure:"postgres"`
	Redis    config.Redis    `mapstructure:"redis"`
	Log      config.Log      `mapstructure:"log"`
	Restate  config.Restate  `mapstructure:"restate"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	return &cfg, config.LoadModule("inventory", &cfg)
}
