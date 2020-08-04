package main

import (
	"github.com/whybangbang/go-gypsy/yaml"
	"mimir/common/zlog"
)

func main() {
	config, err := yaml.ReadFile("/Users/why/go/src/github.com/whybangbang/go-gypsy/example/why/test.yaml")
	if err != nil {
		zlog.Info("err: ", err)
		return
	}
	zlog.Info(config)
}
