package config

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	UcsService = "TCG-UCS-Service"
)

func (c *Config) Init(fileName string) {
	viperConfig := viper.New()

	sanitize := func(s string) string {
		s = strings.ReplaceAll(s, "\n", "")
		s = strings.ReplaceAll(s, "\r", "")
		return s
	}

	env := strings.ToLower(os.Getenv("ENV"))
	env = sanitize(env)
	if env == "" {
		env = "prod"
	}

	var configName string
	switch env {
	case "dev":
		configName = "dev.toml"
	case "sit":
		configName = "sit.toml"
	case "prod":
		configName = "prod.toml"
	default:
		log.Fatal("Unknown environment value (allowed: dev, sit, prod)")
	}

	var searchPaths []string

	if fileName != "" {
		viperConfig.SetConfigFile(fileName)
	} else {
		viperConfig.SetConfigName(configName)
		viperConfig.SetConfigType("toml")

		paths := []string{".", "./config", "../config", "../../config"}
		for _, p := range paths {
			viperConfig.AddConfigPath(p)
			searchPaths = append(searchPaths, p)
		}
	}

	wd, _ := os.Getwd()
	log.Printf("Current working directory     : %s", wd)
	log.Printf("Config name to search for     : %s", configName)
	log.Printf("Search paths (in search order): %v", searchPaths)

	log.Printf("Attempting to load config: %s (env=%s)", configName, env) //nolint:gosec

	// 1st tier default
	{
		viperConfig.SetDefault("env", "pro")
		viperConfig.SetDefault("host", "0.0.0.0")
		viperConfig.SetDefault("port", 18080)
		viperConfig.SetDefault("timeout", 30)           // In second
		viperConfig.SetDefault("bodyLimit", 10485760*5) // 10M * 5
		viperConfig.SetDefault("shutdownTimeout", 30)   // In second
	}

	// 2nd tier Timeouts default
	{
		viperConfig.SetDefault("timeouts.quick", 5)
		viperConfig.SetDefault("timeouts.normal", 30)
		viperConfig.SetDefault("timeouts.long", 60)
		viperConfig.SetDefault("timeouts.upload", 120)
	}

	// 3rd tier default
	{
		viperConfig.SetDefault("oracle.max_open_conn", 1000)
		viperConfig.SetDefault("oracle.max_idle_conn", 1000)
		viperConfig.SetDefault("oracle.max_life_time", 30) // In second
		viperConfig.SetDefault("oracle.max_idle_time", 30) // In minute
		viperConfig.SetDefault("oracle.enable_stats_monitor", true)
		viperConfig.SetDefault("oracle.stats_interval", 60*time.Second) // In second
		viperConfig.SetDefault("oracle.read_timeout", 15*time.Second)   // In second
		viperConfig.SetDefault("oracle.write_timeout", 15*time.Second)  // In second
		viperConfig.SetDefault("oracle.write_timeout", 150*time.Second) // In second
	}
	// 5th tier default
	{
		viperConfig.SetDefault("telemetry.sampler", 1.0)
		viperConfig.SetDefault("telemetry.batcher", "none")
	}
	// 6th tier default
	{
		viperConfig.SetDefault("pprof.enabled", false)
		viperConfig.SetDefault("pprof.port", 6060)
		viperConfig.SetDefault("pprof.host", "0.0.0.0")
	}
	// 7th tier default
	{
		viperConfig.SetDefault("log.mode", "file") // console | file | kafka
		viperConfig.SetDefault("log.env", "pro")
		viperConfig.SetDefault("log.level", "info")
		viperConfig.SetDefault("log.name", "tcg-ace-ae")
		viperConfig.SetDefault("log.serviceName", "TCG-UCS-FE")
		viperConfig.SetDefault("log.encoding", "json")
		viperConfig.SetDefault("log.timeFormat", "2006-01-02 15:04:05.000")
		viperConfig.SetDefault("log.path", "logs")
		viperConfig.SetDefault("log.compress", true)
		viperConfig.SetDefault("log.maxBackups", 10)
		viperConfig.SetDefault("log.maxSize", 500)
		viperConfig.SetDefault("log.stat", true)
		viperConfig.SetDefault("log.keepDays", 5)
		viperConfig.SetDefault("log.rotation", "size")
		viperConfig.SetDefault("log.bufferSize", 30)          // MB
		viperConfig.SetDefault("log.bufferFlushInterval", 50) // ms
	}

	// Read the configuration file
	if err := viperConfig.ReadInConfig(); err != nil {
		log.Fatalf("Failed to read config:\n"+
			"  error       : %v\n"+
			"  config name : %s\n"+
			"  cwd         : %s\n"+
			"  tried paths : %v",
			err, configName, wd, searchPaths)
	}

	if err := viperConfig.Unmarshal(&c); err != nil {
		log.Fatalf("Failed to unmarshal config into struct: %v", err)
	}

	log.Printf("✅ Config loaded successfully, file: %s", viperConfig.ConfigFileUsed())
}
