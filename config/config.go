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

	ClientSecret string `yaml:"client_secret,omitempty"`
	SrcId        string `yaml:"src_cal_id,omitempty"`
	DestId       string `yaml:"dest_cal_id,omitempty"`

	SrcTokenFile  string `yaml:"src_token_file,omitempty"`
	DestTokenFile string `yaml:"dest_token_file,omitempty"`
}

func (c *Config) UseOAuthToken() bool {
	return c.ClientSecret == ""
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

	if c.ClientSecret != "" {
		if c.SrcId == "" || c.DestId == "" {
			log.Fatalf("Not enough calendar id")
		}
	} else {
		if c.SrcTokenFile == "" || c.DestTokenFile == "" {
			log.Fatalf("Not enough OAuth token file")
		}
		c.SrcId = "primary"
		c.DestId = "primary"
	}
	return &c
}
