package main

import (
	"flag"
	"os"

	"github.com/hashicorp/go-hclog"
)

var configFile = flag.String("config", "config.json", "Location of the config file")

func main() {
	flag.Parse()

	logger := hclog.New(&hclog.LoggerOptions{
		Name:       "speedtest-exporter",
		Level:      hclog.Info,
		Output:     os.Stdout,
		JSONFormat: true,
	})

	code := run(logger)
	os.Exit(code)
}
