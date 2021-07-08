package config

import (
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Url     string `yaml:"url"` // webhook url
	Project string `yaml:"project"`
	Rules   []rule `yaml:"rules"`

	SrcTokenFile  string `yaml:"src"`
	DestTokenFile string `yaml:"dest"`

	// Deprecated: need?
	SrcId  string
	DestId string
}

type rule struct {
	Match       string `yaml:"match"`                  // "クリニック"
	StartOffset int    `yaml:"start_offset,omitempty"` // "30" means minute
	EndOffset   int    `yaml:"end_offset,omitempty"`
}

func GetConfig() *Config {
	var c Config

	yamlFile, err := ioutil.ReadFile("env.yaml")
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}
	return &c
}
