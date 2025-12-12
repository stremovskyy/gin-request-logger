# gin-request-logger
Request Logger Middleware for Gin Framework

This is a middleware for [Gin](https://github.com/gin-gonic/gin) framework.

It uses [logrus](https://github.com/sirupsen/logrus) to provide a log features in the package.

## Usage

Download and install using [go module](https://blog.golang.org/using-go-modules):

```sh
$ export GO111MODULE=on
$ go get github.com/stremovskyy/gin-request-logger
```

Import it in your code:

```go
import requestlogger "github.com/stremovskyy/gin-request-logger"
```

## Quick start

```go
router := gin.New()

router.Use(requestlogger.New(requestlogger.Options{
    Pretty:          true,         // pretty-print JSON bodies
    LogHeaders:      true,         // log headers with allow/deny lists
    MaxBodyLogSize:  2048,         // truncate logged body to 2 KB
    LogOnErrorOnly:  false,        // log all responses
    SampleRate:      1.0,          // log 100% of requests
}))
```

### Presets

- `requestlogger.Debug()` – pretty JSON, headers on, larger body limits.
- `requestlogger.Prod()` – compact, log-on-error-only, conservative limits.
- `requestlogger.NewFromEnv(prefix)` – build options from environment (defaults to `REQUEST_LOGGER_`).

### Selected options

- `LogHeaders`, `HeaderAllowlist`, `HeaderDenylist` – control header logging.
- `MaxBodyLogSize` – truncate logged bodies (use negative to disable truncation).
- `MaxRequestBodyBytes`, `MaxResponseBodyBytes` – guard how much body is buffered for logging.
- `LogOnErrorOnly` – log only responses with status >= 400.
- `SampleRate` – percentage of requests to log (set `SampleRate` to `0` and `SampleRateSet` to `true` to disable logging).
- `Structured` – emit structured logrus fields instead of formatted text.
- `RequestIDHeader`, `InjectRequestID`, `RequestIDGenerator` – customize request IDs.
- `ContextFields` – attach structured fields from the current request context.

### Per-request overrides

Use helpers to adjust logging per route/handler:

- `requestlogger.SkipLogging(c)`
- `requestlogger.SkipRequestBody(c)`, `requestlogger.SkipResponseBody(c)`
- `requestlogger.SetBodyLogLimit(c, n)` – override truncation limit
- `requestlogger.SetSampleRate(c, rate)`
- `requestlogger.LogOnlyOnError(c)`
- `requestlogger.AddFields(c, log.Fields{...})`

### Environment variables (when using `NewFromEnv`)

All variables are prefixed (default `REQUEST_LOGGER_`):

- `PRETTY`, `LOG_RESPONSE`, `LOG_HEADERS`, `LOG_ON_ERROR_ONLY`, `STRUCTURED`, `INJECT_REQUEST_ID`
- `MAX_BODY_LOG_SIZE`, `MAX_REQUEST_BODY_BYTES`, `MAX_RESPONSE_BODY_BYTES`, `SAMPLE_RATE`
- `REQUEST_ID_HEADER`, `HEADER_ALLOWLIST`, `HEADER_DENYLIST`
