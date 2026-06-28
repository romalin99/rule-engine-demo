package config

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/bytedance/sonic"
	"github.com/doug-martin/goqu/v9"
	"github.com/redis/go-redis/v9"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"tcg-rulex-engine/internal/client/mcs"
	"tcg-rulex-engine/internal/client/uss"
	"tcg-rulex-engine/internal/client/wps"
	"tcg-rulex-engine/pkg/bigcache"
	"tcg-rulex-engine/pkg/consul"
	"tcg-rulex-engine/pkg/kafka"
	"tcg-rulex-engine/pkg/logs"
	od "tcg-rulex-engine/pkg/oracle"
	"tcg-rulex-engine/pkg/pprof"
	rc "tcg-rulex-engine/pkg/redis"
	"tcg-rulex-engine/pkg/telemetry"
)

type OracleConnectInfo struct {
	OracledbUser            string `json:"oracledb.user"`
	OracledbPassword        string `json:"oracledb.password"`
	OracledbConnectStringer string `json:"oracledb.uconnectStringer"`
}

type AppTimeouts struct {
	Quick  int `mapstructure:"quick"`
	Normal int `mapstructure:"normal"`
	Long   int `mapstructure:"long"`
	Upload int `mapstructure:"upload"`
}

type JobConfig struct {
	Cron        string `mapstructure:"cron"`
	Interval    int    `mapstructure:"interval"`
	Timeout     int    `mapstructure:"timeout"`
	Concurrency int    `mapstructure:"concurrency"`
	Enabled     bool   `mapstructure:"enabled"`
}

type Config struct {
	TracerProvider   *sdktrace.TracerProvider
	Jobs             map[string]JobConfig `mapstructure:"jobs"`
	McsSrv           mcs.Config           `mapstructure:"mcsService"`
	UssSrv           uss.Config           `mapstructure:"ussService"`
	WpsSrv           wps.Config           `mapstructure:"wpsService"`
	Host             string               `mapstructure:"host"`
	Env              string               `mapstructure:"env"`
	Name             string               `mapstructure:"name"`
	Consul           consul.Config        `mapstructure:"consul"`
	TraceIgnorePaths []string             `mapstructure:"traceIgnorePaths"`
	Pprof            pprof.Config         `mapstructure:"pprof"`
	Telemetry        telemetry.Telemetry  `mapstructure:"telemetry"`
	Redis            rc.Config            `mapstructure:"redis"`
	Log              logs.Config          `mapstructure:"log"`
	OracleIns        od.Config            `mapstructure:"oracle"`
	Kafka            kafka.Config         `mapstructure:"kafka"`
	AppTimeouts      AppTimeouts          `mapstructure:"timeouts"`
	BodyLimit        int                  `mapstructure:"bodyLimit"`
	ShutdownTimeout  int                  `mapstructure:"shutdownTimeout"`
	Timeout          int64                `mapstructure:"timeout"`
	BigCache         bigcache.Config      `mapstructure:"bigcache"`
	Port             int                  `mapstructure:"port"`
}

// InitLog initialises the logging system (main log + behavior log when PathBehavior is set).
func (c *Config) InitLog() *logs.Logger {
	c.Log.Env = c.Env
	return logs.NewLogger(c.Log)
}

// LoadOracleConnectInfoFromAws loads Oracle connection credentials from AWS Secrets Manager
func (c *Config) LoadOracleConnectInfoFromAws(envStr string) *OracleConnectInfo {
	ctx := context.Background()
	cfg, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		logs.Fatalf(ctx, "Unable to load AWS Secrets Manager configuration: %v", err)
	}

	client := secretsmanager.NewFromConfig(cfg)

	var secretName string
	switch strings.ToLower(envStr) {
	case "dev":
		secretName = "tcg-uad/db/go-ucs-fe/dev"
	case "sit":
		secretName = "tcg-uad/db/go-ucs-fe/sit"
	case "prod":
		secretName = "tcg-uad/db/go-ucs-fe"
	default:
		logs.Fatalf(ctx, "Unsupported environment variable: %s", envStr)
	}

	result, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId:     &secretName,
		VersionStage: aws.String("AWSCURRENT"),
	})
	if err != nil {
		logs.Fatalf(context.Background(), "Unable to obtain value of key=%v err:%v", secretName, err)
	}

	if result.SecretString == nil {
		logs.Fatalf(ctx, "Key: %s stored value is not in json format", secretName)
		return nil
	}

	var dbCfg OracleConnectInfo
	if err := sonic.UnmarshalString(*result.SecretString, &dbCfg); err != nil {
		logs.Fatalf(ctx, "Unable to unmarshal oracledb info: %v, value=%v", err, *result.SecretString)
		return nil
	}

	return &dbCfg
}

// InitDialect initializes the Oracle SQL dialect for goqu
func InitDialect() {
	opts := goqu.DefaultDialectOptions()
	opts.QuoteRune = ' '
	goqu.RegisterDialect("oracle", opts)
}

// InitRedis initializes and returns a Redis client
func (c *Config) InitRedis() *redis.Client {
	return c.Redis.Init()
}

// InitRedisCluster initializes and returns a Redis cluster client
func (c *Config) InitRedisCluster() *redis.ClusterClient {
	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:          c.Redis.Addr,
		Password:       c.Redis.Password,
		MaxRedirects:   8,
		ReadOnly:       true,
		RouteByLatency: false,
		RouteRandomly:  false,
		DialTimeout:    10 * time.Second,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		PoolSize:       500,
		MinIdleConns:   10,
		PoolTimeout:    30 * time.Second,
		MaxRetries:     2,
	})

	if _, err := clusterClient.Ping(context.Background()).Result(); err != nil {
		logs.Fatalf(context.Background(), "Redis cluster ping failed: %v", err)
	}

	return clusterClient
}

// Close releases all resources held by Config.
// Each shutdown step is guarded by an independent timeout so that an
// unreachable backend (e.g. OTLP collector) cannot block the process
// from exiting.
func (c *Config) Close() {
	c.OracleIns.Close()
	c.Redis.Close()
	c.Telemetry.Close() // already has a 5 s internal timeout

	// TracerProvider.Shutdown flushes in-flight spans to the OTLP backend.
	// Use an explicit 5 s deadline so the process is not held hostage when
	// the backend is unreachable.
	if c.TracerProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.TracerProvider.Shutdown(ctx); err != nil {
			logs.Warn(context.Background(), "error shutting down TracerProvider: %v", err)
		}
	}
}
