package main

import (
	"os"

	"github.com/utilitywarehouse/terraform-applier/git"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Repositories map[string]git.RepositoryConfig `yaml:"repositories"`
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
