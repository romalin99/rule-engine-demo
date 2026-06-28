package router

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"

	swaggo "github.com/gofiber/contrib/v3/swaggo"
	"github.com/gofiber/fiber/v3"

	docs "tcg-rulex-engine/docs"
	"tcg-rulex-engine/internal/config"
	"tcg-rulex-engine/pkg/logs"
)

// Init registers the Swagger UI using the full application config. Kept for
// callers that boot the server with a *config.Config; the spec Host is built
// from c.Port (the host part is overridable via the SWAGGER_HOST env var).
func Init(app *fiber.App, c *config.Config) {
	registerSwagger(app, c.Port)
}

// RegisterSwagger mounts the Swagger UI on app. port is used only to populate
// the spec's Host field (the "Try it out" base URL) and is normally derived
// from the listen address. The host part can be overridden with SWAGGER_HOST.
func RegisterSwagger(app *fiber.App, port int) {
	registerSwagger(app, port)
}

// registerSwagger resolves the host IP, populates the SwaggerInfo metadata for
// the AIRuleX rule engine, and mounts the UI at /swagger/*.
func registerSwagger(app *fiber.App, port int) {
	host := os.Getenv("SWAGGER_HOST")
	if host == "" {
		host = getLocalIP()
	}
	if host == "" {
		host = getOutboundIP()
	}

	docs.SwaggerInfo.Host = net.JoinHostPort(host, strconv.Itoa(port))
	docs.SwaggerInfo.BasePath = BasePath
	docs.SwaggerInfo.Title = "AIRuleX 规则引擎 API"
	docs.SwaggerInfo.Description = "AIRuleX 实时规则引擎 HTTP 接口：在线评分（/match、/match/batch、/evaluate）、规则热更新（/rules*）与版本管理（/versions*）。"
	docs.SwaggerInfo.Version = "2.0"
	docs.SwaggerInfo.Schemes = []string{"http"} // add "https" in production

	// Mount the Swagger UI; the spec is served automatically from the docs package.
	app.Get("/swagger/*", swaggo.New(swaggo.Config{
		Title:                    "AIRuleX 规则引擎 API 文档",
		DeepLinking:              true,   // enable deep-linking to individual operations
		DocExpansion:             "list", // "list" | "full" | "none"
		DefaultModelsExpandDepth: -1,     // -1 = collapse models, 1 = expand one level
		DefaultModelExpandDepth:  1,
		DisplayOperationId:       true,   // show operationId (useful for debugging)
		DisplayRequestDuration:   true,   // show per-request latency (useful in development)
		PersistAuthorization:     false,  // do not persist Authorization header (recommended for production)
		ValidatorUrl:             "none", // close the validator badge in the bottom-right corner
		CustomStyle: `
		.opblock-summary-operation-id {
		   word-break: keep-all !important;
		}
		`,
	}))
}

// portFromAddr extracts the TCP port from a listen address such as ":8080" or
// "0.0.0.0:8080". It falls back to 8080 when the address has no parseable port.
func portFromAddr(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		portStr = strings.TrimPrefix(addr, ":")
	}
	if p, perr := strconv.Atoi(portStr); perr == nil {
		return p
	}
	return 8080
}

// getOutboundIP returns the preferred outbound IP address of the host by
// opening a UDP connection to a well-known external address (8.8.8.8:80).
// No data is actually sent; the OS merely selects the appropriate local interface.
// Falls back to getLocalIP if the UDP dial fails.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return getLocalIP()
	}

	defer func() {
		if err := conn.Close(); err != nil {
			logs.Warn(context.Background(), "conn.Close failed: %v", err)
		}
	}()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// getLocalIP returns the first non-loopback IPv4 address found on the host's
// network interfaces. It returns "localhost" if no suitable address is found
// or if interface enumeration fails.
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "localhost"
}
