package main

import (
	"os"

	"github.com/utilitywarehouse/git-mirror/repopool"
	"gopkg.in/yaml.v2"
)

type Config struct {
	GitMirror repopool.Config `yaml:"git_mirror"`
}

func parseConfigFile(path string) (*Config, error) {
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	conf := &Config{}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}
