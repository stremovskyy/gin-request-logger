# gin-request-logger
Request/response logger middleware for [Gin](https://github.com/gin-gonic/gin) powered by [logrus](https://github.com/sirupsen/logrus).

## Highlights
- Gzip-aware response logging (auto-decompresses when possible).
- Binary/multipart detection with safe placeholders (no junk in logs).
- Body truncation with configurable limits (defaults to 1 KB).
- Header logging with allow/deny lists.
- Request ID injection/customization.
- Sampling, log-on-error-only, and per-request overrides.
- Structured or colored text output.
- Presets for debug/prod and env-driven configuration.

## Install

```sh
go get github.com/stremovskyy/gin-request-logger
```

## Quick start

```go
import (
    "github.com/gin-gonic/gin"
    requestlogger "github.com/stremovskyy/gin-request-logger"
)

router := gin.New()
router.Use(requestlogger.New(requestlogger.Options{
    Pretty:         true,  // pretty-print JSON bodies
    LogHeaders:     true,  // log selected headers
    MaxBodyLogSize: 2048, // truncate logged bodies to 2 KB
}))
```

## Presets

```go
router.Use(requestlogger.Debug()) // Pretty, headers on, larger limits
router.Use(requestlogger.Prod())  // Compact, log-on-error-only, conservative limits
```

## Common options

- `LogRequest`, `LogResponse` – enable/disable request/response logging.
- `Pretty` – pretty-print JSON bodies.
- `MaxBodyLogSize` – truncate logged bodies (negative to disable).
- `MaxRequestBodyBytes`, `MaxResponseBodyBytes` – cap how much data is buffered for logging.
- `LogHeaders`, `HeaderAllowlist`, `HeaderDenylist` – control which headers appear in logs.
- `LogOnErrorOnly` – only log responses with status >= 400.
- `SampleRate` (+ `SampleRateSet`) – percentage of requests to log (set `SampleRate: 0, SampleRateSet: true` to disable).
- `Structured` – emit structured logrus fields instead of colored text.
- `RequestIDHeader`, `InjectRequestID`, `RequestIDGenerator` – customize request IDs.
- `ContextFields` – attach structured fields derived from the current request.

## Per-request overrides

```go
func handler(c *gin.Context) {
    requestlogger.SkipRequestBody(c)          // omit request body
    requestlogger.SkipResponseBody(c)         // omit response body
    requestlogger.SetBodyLogLimit(c, 512)     // tighter per-request limit
    requestlogger.SetSampleRate(c, 0.25)      // sample only 25% for this request
    requestlogger.LogOnlyOnError(c)           // log only if status >= 400
    requestlogger.AddFields(c, log.Fields{
        "user_id": "123",
    })
    c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

To skip everything for a route:

```go
func healthz(c *gin.Context) {
    requestlogger.SkipLogging(c)
    c.String(http.StatusOK, "ok")
}
```

## Env configuration

Build from env with `requestlogger.NewFromEnv("REQUEST_LOGGER_")` (prefix is optional; default is `REQUEST_LOGGER_`).

Supported variables:

- `PRETTY`, `LOG_RESPONSE`, `LOG_HEADERS`, `LOG_ON_ERROR_ONLY`, `STRUCTURED`, `INJECT_REQUEST_ID`
- `MAX_BODY_LOG_SIZE`, `MAX_REQUEST_BODY_BYTES`, `MAX_RESPONSE_BODY_BYTES`, `SAMPLE_RATE`
- `REQUEST_ID_HEADER`, `HEADER_ALLOWLIST`, `HEADER_DENYLIST`

## Examples

Basic structured logging:

```go
router.Use(requestlogger.New(requestlogger.Options{
    Structured: true,
    LogHeaders: true,
    ContextFields: func(c *gin.Context) log.Fields {
        return log.Fields{"request_id": c.GetString("request_id")}
    },
}))
```

Only log errors with tight limits:

```go
router.Use(requestlogger.New(requestlogger.Options{
    LogOnErrorOnly:       true,
    MaxBodyLogSize:       512,
    MaxRequestBodyBytes:  1 << 20, // 1 MB capture guard
    MaxResponseBodyBytes: 1 << 20,
}))
```
