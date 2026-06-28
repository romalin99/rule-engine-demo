package middleware

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel/trace"

	"tcg-rulex-engine/internal/masking"
	"tcg-rulex-engine/pkg/constant"
	"tcg-rulex-engine/pkg/logs"
)

// msgBuilderPool reuses strings.Builder instances across requests to eliminate
// per-request heap allocations in the behavior log hot path (called on every
// non-skipped HTTP request).
var msgBuilderPool = sync.Pool{
	New: func() any {
		sb := &strings.Builder{}
		sb.Grow(256) // pre-size for a typical behavior log line
		return sb
	},
}

// extractTraceIDs returns the traceID and spanID for the current request.
// It checks the OTel span first, then falls back to well-known HTTP headers.
func extractTraceIDs(ctx context.Context, c fiber.Ctx) (traceID, spanID string) {
	if span := trace.SpanFromContext(ctx); span != nil && span.SpanContext().IsValid() {
		sc := span.SpanContext()
		return sc.TraceID().String(), sc.SpanID().String()
	}

	if c != nil {
		// X-App-Trace-ID is the canonical application trace header (WPS Relay Pass Header).
		traceID = c.Get("X-App-Trace-ID")
		spanID = c.Get("X-Span-Id")
		if traceID == "" {
			traceID = c.Get("X-Trace-Id")
		}
		if traceID == "" {
			traceID = c.Get("uber-trace-id")
		}
		if traceID == "" {
			traceID = c.Get("traceparent")
		}
	}
	if traceID == "" {
		traceID = "unknown"
	}
	if spanID == "" {
		spanID = "unknown"
	}
	return traceID, spanID
}

// BehaviorLogger logs every non-skipped HTTP request to the behavior log file.
// skipPrefixes is checked with strings.HasPrefix (O(1) string comparison per
// entry) rather than strings.Contains to avoid false positives on path
// sub-strings and to be explicit about intent.
// The set is stored as a plain []string because the list is tiny (≤ 10 entries)
// and CPU-branch-prediction-friendly sequential comparison is faster than a map
// hash for such small N.
type BehaviorLogger struct {
	serviceName  string
	skipPrefixes []string
}

func NewBehaviorLogger(serviceName string) *BehaviorLogger {
	return &BehaviorLogger{
		serviceName: serviceName,
		skipPrefixes: []string{
			"/test/",
			"/metrics",
			"/swagger",
			"/favicon.ico",
			"/health",
			"/livez",
			"/readyz",
			"/ping",
			"/monitor",
		},
	}
}

func (l *BehaviorLogger) Handle() fiber.Handler {
	return func(c fiber.Ctx) error {
		path := c.Path()
		for _, prefix := range l.skipPrefixes {
			if strings.HasPrefix(path, prefix) {
				return c.Next()
			}
		}

		// X-App-Trace-ID (WPS Relay Pass Header) takes highest priority so that
		// callers can correlate logs using their own trace token.  Only fall back
		// to the OTel span trace ID when the header is absent.
		traceID := c.Get("X-App-Trace-ID")
		if traceID == "" {
			traceID, _ = extractTraceIDs(c.Context(), c)
		}
		c.SetContext(context.WithValue(c.Context(), constant.CtxTraceID, traceID))

		// Log the incoming request before the handler runs.
		l.logIncomingRequest(c)

		start := time.Now()
		err := c.Next()

		// Echo the trace token back in the response so callers can correlate
		// their own response logs with the upstream request chain.
		c.Set("X-App-Trace-ID", traceID)

		l.logRequest(c, start)
		return err
	}
}

// logIncomingRequest writes one structured log line at the moment the request
// arrives, before any handler logic runs.  For GET requests the query string
// is included; for requests with a body (POST/PUT/PATCH) the body is masked
// via masking.RequestBody so sensitive fieldValue entries are redacted before
// the line is written to the log file.
func (l *BehaviorLogger) logIncomingRequest(c fiber.Ctx) {
	ctx := c.Context()
	if body := c.Body(); len(body) > 0 {
		logs.Info(ctx, "[API-REQUEST] [START] method=%s uri=%s body=%s addr=%s",
			c.Method(), c.OriginalURL(), string(masking.RequestBody(body)), getClientIP(c))
	} else {
		logs.Info(ctx, "[API-REQUEST] [START] method=%s uri=%s addr=%s",
			c.Method(), c.OriginalURL(), getClientIP(c))
	}
}

func (l *BehaviorLogger) logRequest(c fiber.Ctx, start time.Time) {
	ctx := c.Context()
	traceID, spanID := extractTraceIDs(ctx, c)

	statusCode := c.Response().StatusCode()
	elapsed := time.Since(start)

	// Structured response log — ctx carries trace_id so the field is appended
	// automatically by appendContextFields on every logs.Info/Warn/Err call.
	// Response body is included so the full input→output round-trip is visible
	// in the log without needing to cross-reference multiple sources.
	logMsg := "[API-RESPONSE] [END] method=%s uri=%s status=%d elapsed=%dms addr=%s body=%s"
	logArgs := []any{c.Method(), c.OriginalURL(), statusCode, elapsed.Milliseconds(), getClientIP(c), string(c.Response().Body())}
	switch {
	case statusCode >= 500:
		logs.Err(ctx, logMsg, logArgs...)
	case statusCode >= 400:
		logs.Warn(ctx, logMsg, logArgs...)
	default:
		logs.Info(ctx, logMsg, logArgs...)
	}

	// Use a pooled builder to avoid a heap allocation from fmt.Sprintf on
	// every request. The builder is reset before use and returned to the
	// pool after the log call, so it is never shared between goroutines.
	sb := msgBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	sb.WriteString("[")
	sb.WriteString(traceID)
	sb.WriteString("/")
	sb.WriteString(spanID)
	sb.WriteString("] [API-REQUEST] URI: ")
	sb.WriteString(c.Path())
	sb.WriteString(", Method: ")
	sb.WriteString(c.Method())
	sb.WriteString(", Status: ")
	sb.WriteString(strconv.Itoa(statusCode))
	sb.WriteString(", Addr: ")
	sb.WriteString(getClientIP(c))
	sb.WriteString(", Elapsed: ")
	sb.WriteString(strconv.FormatInt(elapsed.Milliseconds(), 10))
	sb.WriteString("ms")
	msg := sb.String()
	msgBuilderPool.Put(sb)

	switch {
	case statusCode >= 500:
		logs.BehaviorError(msg)
	case statusCode >= 400:
		logs.BehaviorWarn(msg)
	default:
		logs.BehaviorInfo(msg)
	}
}

// getClientIP returns the client's real IP address.
// It checks the CustomerIP WPS Relay Pass Header first; if present its value is
// used directly (the upstream relay has already resolved the real client IP).
// Otherwise it prefers the rightmost public IP in X-Forwarded-For to resist
// spoofing, then falls back to X-Real-IP and finally to the direct remote address.
func getClientIP(c fiber.Ctx) string {
	if customerIP := strings.TrimSpace(c.Get("CustomerIP")); customerIP != "" {
		return customerIP
	}

	if xff := strings.TrimSpace(c.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			if ip := strings.TrimSpace(parts[i]); isPublicIP(ip) {
				return ip
			}
		}
	}

	if xrip := strings.TrimSpace(c.Get("X-Real-IP")); xrip != "" && isPublicIP(xrip) {
		return xrip
	}

	return c.IP()
}

// isPublicIP returns true when ip is a valid, globally-routable address.
func isPublicIP(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return addr.IsGlobalUnicast() && !addr.IsPrivate()
}
