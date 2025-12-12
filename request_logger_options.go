package gin_request_logger

import (
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// Options configures the request logger middleware.
type Options struct {
	// IsDebug toggles debug behavior (legacy; prefer the Debug preset).
	IsDebug bool

	// LogRequest enables request logging.
	LogRequest bool

	// LogResponse enables response logging.
	LogResponse bool

	// Pretty pretty-prints JSON bodies.
	Pretty bool

	// LogHeaders toggles request/response header logging.
	LogHeaders bool

	// HeaderAllowlist limits which headers get logged (case-insensitive).
	HeaderAllowlist []string

	// HeaderDenylist blocks specific headers from being logged (case-insensitive).
	HeaderDenylist []string

	// LogOnErrorOnly logs only when status >= 400.
	LogOnErrorOnly bool

	// Structured outputs logs as structured fields via logrus.
	Structured bool

	// SampleRate controls percentage of requests to log (0-1).
	SampleRate float64

	// SampleRateSet indicates SampleRate was intentionally set (allows zero).
	SampleRateSet bool

	// MaxBodyLogSize caps how many bytes from the body are written to logs.
	// Use a negative value to disable truncation.
	MaxBodyLogSize int

	// MaxRequestBodyBytes skips request body logging when the Content-Length exceeds this limit.
	// Use a negative value to disable the guard.
	MaxRequestBodyBytes int64

	// MaxResponseBodyBytes caps how much of the response body is buffered for logging.
	// Use a negative value to disable the guard.
	MaxResponseBodyBytes int64

	// RequestIDHeader allows customizing the header to read/write the request ID.
	RequestIDHeader string

	// InjectRequestID injects the request ID header when missing.
	InjectRequestID bool

	// RequestIDGenerator allows custom request ID creation when the header is absent.
	RequestIDGenerator func(*gin.Context) string

	// ContextFields lets callers attach structured fields for every log line.
	ContextFields func(*gin.Context) log.Fields
}
