package gin_request_logger

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

var (
	requestCounter uint64
	mutex          sync.Mutex

	randMu  sync.Mutex
	randSrc = rand.New(rand.NewSource(time.Now().UnixNano()))
)

const (
	// ANSI color escape sequences
	ColorReset  = "\033[0m"
	ColorCyan   = "\033[36m"
	ColorYellow = "\033[33m"
	ColorGreen  = "\033[32m"
	ColorRed    = "\033[31m"

	defaultMaxBodyLogSize      = 1024 // 1 KB
	defaultRequestBodyBytes    = int64(1 << 20)
	defaultResponseBodyBytes   = int64(1 << 20)
	defaultSampleRate          = 1.0
	defaultRequestIDHeaderName = "X-Request-ID"
)

type ctxKey string

const (
	ctxSkipLoggingKey    ctxKey = "reqlog_skip_logging"
	ctxSkipReqBodyKey    ctxKey = "reqlog_skip_request_body"
	ctxSkipRespBodyKey   ctxKey = "reqlog_skip_response_body"
	ctxBodyLogLimitKey   ctxKey = "reqlog_body_log_limit"
	ctxSampleRateKey     ctxKey = "reqlog_sample_rate"
	ctxLogOnErrorOnlyKey ctxKey = "reqlog_log_on_error_only"
	ctxFieldsKey         ctxKey = "reqlog_fields"
)

// New returns a handler with the provided options (defaults applied where missing).
func New(options Options) gin.HandlerFunc {
	return RequestLoggerWithOptions(options)
}

// NewFromEnv builds the middleware reading options from environment variables.
// Prefix controls variable names, e.g. "REQUEST_LOGGER_" yields REQUEST_LOGGER_PRETTY, etc.
func NewFromEnv(prefix string) gin.HandlerFunc {
	return RequestLoggerWithOptions(OptionsFromEnv(prefix))
}

// RequestLogger preserves the original signature while adding sensible defaults.
func RequestLogger(pretty bool) gin.HandlerFunc {
	opts := defaultOptions()
	opts.Pretty = pretty
	return RequestLoggerWithOptions(opts)
}

// Debug preset for local debugging.
func Debug() gin.HandlerFunc {
	return RequestLoggerWithOptions(Options{
		Pretty:               true,
		LogRequest:           true,
		LogResponse:          true,
		LogHeaders:           true,
		MaxBodyLogSize:       4 * defaultMaxBodyLogSize,
		MaxRequestBodyBytes:  4 * defaultRequestBodyBytes,
		MaxResponseBodyBytes: 4 * defaultResponseBodyBytes,
		InjectRequestID:      true,
		SampleRate:           defaultSampleRate,
	})
}

// Prod preset for production with restrained logging.
func Prod() gin.HandlerFunc {
	return RequestLoggerWithOptions(Options{
		Pretty:               false,
		LogRequest:           true,
		LogResponse:          true,
		LogHeaders:           false,
		LogOnErrorOnly:       true,
		MaxBodyLogSize:       defaultMaxBodyLogSize,
		MaxRequestBodyBytes:  defaultRequestBodyBytes,
		MaxResponseBodyBytes: defaultResponseBodyBytes,
		InjectRequestID:      true,
		SampleRate:           defaultSampleRate,
	})
}

func RequestLoggerWithOptions(options Options) gin.HandlerFunc {
	opts := applyDefaults(options)

	return func(c *gin.Context) {
		// Request ID setup
		requestID := getRequestID(c, opts)

		if getCtxBool(c, ctxSkipLoggingKey) {
			c.Next()
			return
		}

		sampleRate := effectiveSampleRate(c, opts.SampleRate)
		if !shouldSample(sampleRate) {
			c.Next()
			return
		}

		// Capture request body (if allowed)
		reqCapture := captureRequestBody(c, opts)

		// Capture request headers early
		var reqHeaders http.Header
		if opts.LogHeaders {
			reqHeaders = filterHeaders(c.Request.Header, opts.HeaderAllowlist, opts.HeaderDenylist)
		}

		// Response writer with bounded capture buffer
		respBuffer := newLimitedBuffer(opts.MaxResponseBodyBytes)
		writer := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           respBuffer,
		}
		c.Writer = writer

		// Process the request
		c.Next()

		status := c.Writer.Status()
		if logOnlyErrors(c, opts.LogOnErrorOnly) && status < http.StatusBadRequest {
			return
		}

		maxBodyLogSize := effectiveMaxBodyLogSize(c, opts.MaxBodyLogSize)

		contextFields := collectContextFields(c, opts)

		// Log request line + body
		if opts.LogRequest {
			logRequest(c, opts, requestID, reqCapture, reqHeaders, maxBodyLogSize, contextFields)
		}

		// Log response body + headers
		if opts.LogResponse && !getCtxBool(c, ctxSkipRespBodyKey) {
			logResponse(c, opts, requestID, writer, maxBodyLogSize, contextFields)
		}

		// Log status
		logStatus(c, opts, requestID, status, contextFields)
	}
}

func logRequest(c *gin.Context, opts Options, requestID string, reqBody capturedBody, reqHeaders http.Header, maxBodyLogSize int, ctxFields log.Fields) {
	urlStr := c.Request.URL.String()
	params := getURLParameters(c.Request.URL)

	skipBody := getCtxBool(c, ctxSkipReqBodyKey)
	bodyForLog := formatRequestBodyForLog(reqBody, opts.Pretty, maxBodyLogSize, skipBody)

	if opts.Structured {
		fields := cloneFields(ctxFields)
		fields["phase"] = "request"
		fields["request_id"] = requestID
		fields["url"] = urlStr
		fields["params"] = params
		if opts.LogHeaders && reqHeaders != nil {
			fields["headers"] = reqHeaders
		}
		fields["body"] = bodyForLog
		log.WithFields(fields).Trace("http request")
		return
	}

	log.Trace(ColorCyan + "╔═════HTTP REQUEST\n")
	log.Trace(fmt.Sprintf("[%s] URL: %s", requestID, urlStr))
	log.Trace(fmt.Sprintf("[%s] Parameters: %s", requestID, params))
	if opts.LogHeaders && reqHeaders != nil {
		log.Trace(fmt.Sprintf("[%s] Headers: %s", requestID, headersToString(reqHeaders)))
	}
	log.Trace(fmt.Sprintf("[%s] %s", requestID, bodyForLog))
	log.Trace(ColorCyan + "╚═════END HTTP REQUEST" + ColorReset)
}

func logResponse(c *gin.Context, opts Options, requestID string, writer *responseBodyWriter, maxBodyLogSize int, ctxFields log.Fields) {
	headers := writer.Header()
	body := formatResponseBodyForLog(headers, writer.body.Bytes(), opts.Pretty, maxBodyLogSize, writer.body.Truncated())
	if opts.Structured {
		fields := cloneFields(ctxFields)
		fields["phase"] = "response"
		fields["request_id"] = requestID
		fields["status"] = c.Writer.Status()
		fields["status_text"] = http.StatusText(c.Writer.Status())
		if opts.LogHeaders {
			fields["headers"] = filterHeaders(headers, opts.HeaderAllowlist, opts.HeaderDenylist)
		}
		fields["body"] = body
		log.WithFields(fields).Trace("http response")
		return
	}

	log.Trace(ColorYellow + "╔═════HTTP RESPONSE\n")
	log.Trace(fmt.Sprintf("[%s] %s", requestID, body))
	if opts.LogHeaders {
		log.Trace(fmt.Sprintf("[%s] Headers: %s", requestID, headersToString(filterHeaders(headers, opts.HeaderAllowlist, opts.HeaderDenylist))))
	}
	log.Trace(ColorYellow + "╚═════END HTTP RESPONSE" + ColorReset)
}

func logStatus(c *gin.Context, opts Options, requestID string, status int, ctxFields log.Fields) {
	statusColor := getStatusColor(status)
	if opts.Structured {
		fields := cloneFields(ctxFields)
		fields["phase"] = "response_status"
		fields["request_id"] = requestID
		fields["status"] = status
		fields["status_text"] = http.StatusText(status)
		log.WithFields(fields).Trace("http response status")
		return
	}

	log.Trace(statusColor + "╔═════HTTP RESPONSE STATUS\n")
	log.Trace(fmt.Sprintf("[%s] Status: %d %s", requestID, status, http.StatusText(status)))
	log.Trace(statusColor + "╚═════END HTTP RESPONSE STATUS" + ColorReset)
}

func captureRequestBody(c *gin.Context, opts Options) capturedBody {
	ct := c.Request.Header.Get("Content-Type")

	if opts.MaxRequestBodyBytes > 0 && c.Request.ContentLength > opts.MaxRequestBodyBytes {
		return capturedBody{
			contentType: ct,
			placeholder: fmt.Sprintf("[request body skipped: content-length %d exceeds limit %d bytes]", c.Request.ContentLength, opts.MaxRequestBodyBytes),
		}
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return capturedBody{
			contentType: ct,
			placeholder: fmt.Sprintf("[request body read error: %v]", err),
		}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	return capturedBody{
		content:     body,
		contentType: ct,
	}
}

func filterHeaders(headers http.Header, allowlist []string, denylist []string) http.Header {
	if len(headers) == 0 {
		return nil
	}

	if len(allowlist) == 0 && len(denylist) == 0 {
		return headers
	}

	allow := make(map[string]struct{})
	deny := make(map[string]struct{})

	for _, h := range allowlist {
		allow[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}
	for _, h := range denylist {
		deny[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}

	out := http.Header{}
	for k, v := range headers {
		lower := strings.ToLower(k)
		if len(allow) > 0 {
			if _, ok := allow[lower]; !ok {
				continue
			}
		}
		if _, blocked := deny[lower]; blocked {
			continue
		}
		out[k] = v
	}

	return out
}

func formatResponseBodyForLog(headers http.Header, body []byte, pretty bool, limit int, truncated bool) string {
	ct := headers.Get("Content-Type")

	if isGzipEncoding(headers.Get("Content-Encoding")) {
		if truncated {
			return fmt.Sprintf("[response body omitted: gzip body exceeded capture limit]")
		}
		decoded, err := gunzip(body)
		if err == nil {
			return formatLoggedBody(decoded, ct, pretty, limit, truncated, "")
		}
		log.Debugf("failed to gunzip response body for logging: %v", err)
	}

	return formatLoggedBody(body, ct, pretty, limit, truncated, "")
}

func formatRequestBodyForLog(body capturedBody, pretty bool, limit int, skipBody bool) string {
	if skipBody {
		return "[request body skipped by override]"
	}
	if body.placeholder != "" {
		return body.placeholder
	}
	return formatLoggedBody(body.content, body.contentType, pretty, limit, false, "")
}

func formatLoggedBody(body []byte, contentType string, pretty bool, limit int, truncated bool, placeholder string) string {
	if placeholder != "" {
		return placeholder
	}

	formatted := prepareBodyForLog(body, contentType, pretty, limit)
	if truncated {
		return formatted + " [capture truncated]"
	}
	return formatted
}

func printData(content string, pretty bool) string {
	if pretty {
		return formatJSON(content)
	}

	return content
}

func formatJSON(content string) string {
	var data interface{}
	err := json.Unmarshal([]byte(content), &data)
	if err != nil {
		return content
	}
	prettyJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return content
	}
	return string(prettyJSON)
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *limitedBuffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	_, _ = w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func getURLParameters(u *url.URL) string {
	params := u.Query()
	if len(params) == 0 {
		return "None"
	}
	var paramStrings []string
	for key, values := range params {
		for _, value := range values {
			paramStrings = append(paramStrings, key+"="+value)
		}
	}
	sort.Strings(paramStrings)
	return strings.Join(paramStrings, ", ")
}

func getStatusColor(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return ColorGreen
	case statusCode >= 300 && statusCode < 400:
		return ColorCyan
	case statusCode >= 400 && statusCode < 500:
		return ColorYellow
	default:
		return ColorRed
	}
}

func getRequestID(c *gin.Context, opts Options) string {
	headerName := opts.RequestIDHeader
	if headerName == "" {
		headerName = defaultRequestIDHeaderName
	}

	requestID := c.GetHeader(headerName)

	if requestID == "" && opts.RequestIDGenerator != nil {
		requestID = opts.RequestIDGenerator(c)
	}

	if requestID == "" {
		mutex.Lock()
		requestCounter++
		requestID = fmt.Sprintf("REQ-%d", requestCounter)
		mutex.Unlock()
	}

	if opts.InjectRequestID {
		c.Request.Header.Set(headerName, requestID)
		c.Header(headerName, requestID)
	}

	c.Set("request_id", requestID)

	return requestID
}

func prepareBodyForLog(body []byte, contentType string, pretty bool, limit int) string {
	if isBinaryContent(contentType, body) {
		return binaryPlaceholder(len(body), contentType)
	}

	formatted := printData(string(body), pretty)
	return truncateContent(formatted, limit)
}

func truncateContent(content string, limit int) string {
	if limit < 0 || len(content) <= limit {
		return content
	}

	return content[:limit] + fmt.Sprintf("... [truncated to %d bytes]", limit)
}

func gunzip(content []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func isGzipEncoding(encoding string) bool {
	return strings.Contains(strings.ToLower(encoding), "gzip")
}

func isBinaryContent(contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "multipart/"),
		strings.Contains(ct, "octet-stream"),
		strings.HasPrefix(ct, "image/"),
		strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "video/"),
		strings.Contains(ct, "zip"),
		strings.Contains(ct, "pdf"),
		strings.Contains(ct, "protobuf"),
		strings.Contains(ct, "excel"),
		strings.Contains(ct, "word"),
		strings.Contains(ct, "powerpoint"),
		strings.Contains(ct, "ms-"),
		strings.Contains(ct, "avro"),
		strings.Contains(ct, "vnd"):
		return true
	}

	if len(body) == 0 {
		return false
	}

	sample := body
	if len(sample) > 512 {
		sample = sample[:512]
	}

	printable := 0
	for _, b := range sample {
		if (b >= 0x20 && b <= 0x7e) || b == '\n' || b == '\r' || b == '\t' {
			printable++
		}
	}

	return printable < len(sample)*8/10
}

func binaryPlaceholder(size int, contentType string) string {
	if contentType == "" {
		contentType = "unknown"
	}

	return fmt.Sprintf("[binary content omitted: %d bytes, content-type=%s]", size, contentType)
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit < 0 {
		return b.buf.Write(p)
	}

	remaining := b.limit - int64(b.buf.Len())
	if remaining > 0 {
		toWrite := len(p)
		if int64(toWrite) > remaining {
			toWrite = int(remaining)
		}
		_, _ = b.buf.Write(p[:toWrite])
	}
	if int64(b.buf.Len()) >= b.limit && len(p) > int(remaining) {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

type capturedBody struct {
	content     []byte
	contentType string
	placeholder string
}

func defaultOptions() Options {
	return Options{
		LogRequest:           true,
		LogResponse:          true,
		Pretty:               false,
		LogHeaders:           false,
		MaxBodyLogSize:       defaultMaxBodyLogSize,
		MaxRequestBodyBytes:  defaultRequestBodyBytes,
		MaxResponseBodyBytes: defaultResponseBodyBytes,
		SampleRate:           defaultSampleRate,
		SampleRateSet:        true,
		RequestIDHeader:      defaultRequestIDHeaderName,
		InjectRequestID:      true,
	}
}

func applyDefaults(opts Options) Options {
	if opts.SampleRate != 0 {
		opts.SampleRateSet = true
	}
	if !opts.LogRequest && !opts.LogResponse {
		opts.LogRequest = true
		opts.LogResponse = true
	}
	if opts.MaxBodyLogSize == 0 {
		opts.MaxBodyLogSize = defaultMaxBodyLogSize
	}
	if opts.MaxRequestBodyBytes == 0 {
		opts.MaxRequestBodyBytes = defaultRequestBodyBytes
	}
	if opts.MaxResponseBodyBytes == 0 {
		opts.MaxResponseBodyBytes = defaultResponseBodyBytes
	}
	if !opts.SampleRateSet {
		opts.SampleRate = defaultSampleRate
	}
	if opts.RequestIDHeader == "" {
		opts.RequestIDHeader = defaultRequestIDHeaderName
	}
	return opts
}

func getCtxBool(c *gin.Context, key ctxKey) bool {
	val, ok := c.Get(string(key))
	if !ok {
		return false
	}
	boolean, ok := val.(bool)
	return ok && boolean
}

func effectiveMaxBodyLogSize(c *gin.Context, defaultLimit int) int {
	val, ok := c.Get(string(ctxBodyLogLimitKey))
	if !ok {
		return defaultLimit
	}
	if limit, ok := val.(int); ok {
		return limit
	}
	return defaultLimit
}

func effectiveSampleRate(c *gin.Context, defaultRate float64) float64 {
	val, ok := c.Get(string(ctxSampleRateKey))
	if !ok {
		return defaultRate
	}
	if rate, ok := val.(float64); ok {
		return rate
	}
	return defaultRate
}

func shouldSample(rate float64) bool {
	if rate >= 1 {
		return true
	}
	if rate <= 0 {
		return false
	}
	randMu.Lock()
	defer randMu.Unlock()
	return randSrc.Float64() <= rate
}

func logOnlyErrors(c *gin.Context, defaultValue bool) bool {
	if getCtxBool(c, ctxLogOnErrorOnlyKey) {
		return true
	}
	return defaultValue
}

func headersToString(headers http.Header) string {
	if len(headers) == 0 {
		return "None"
	}
	var pairs []string
	for k, v := range headers {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, strings.Join(v, ",")))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}

func collectContextFields(c *gin.Context, opts Options) log.Fields {
	fields := log.Fields{}
	if opts.ContextFields != nil {
		for k, v := range opts.ContextFields(c) {
			fields[k] = v
		}
	}
	if extra, ok := c.Get(string(ctxFieldsKey)); ok {
		if m, ok := extra.(log.Fields); ok {
			for k, v := range m {
				fields[k] = v
			}
		}
	}
	return fields
}

func cloneFields(fields log.Fields) log.Fields {
	if len(fields) == 0 {
		return log.Fields{}
	}
	out := make(log.Fields, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// SkipLogging skips logging for the current request.
func SkipLogging(c *gin.Context) {
	c.Set(string(ctxSkipLoggingKey), true)
}

// SkipRequestBody skips request body logging for the current request.
func SkipRequestBody(c *gin.Context) {
	c.Set(string(ctxSkipReqBodyKey), true)
}

// SkipResponseBody skips response body logging for the current request.
func SkipResponseBody(c *gin.Context) {
	c.Set(string(ctxSkipRespBodyKey), true)
}

// SetBodyLogLimit overrides the body log limit for the current request.
func SetBodyLogLimit(c *gin.Context, limit int) {
	c.Set(string(ctxBodyLogLimitKey), limit)
}

// SetSampleRate overrides the sample rate for the current request.
func SetSampleRate(c *gin.Context, rate float64) {
	c.Set(string(ctxSampleRateKey), rate)
}

// LogOnlyOnError enables log-on-error-only mode for the current request.
func LogOnlyOnError(c *gin.Context) {
	c.Set(string(ctxLogOnErrorOnlyKey), true)
}

// AddFields attaches structured fields for the current request.
func AddFields(c *gin.Context, fields log.Fields) {
	if fields == nil {
		return
	}
	current, _ := c.Get(string(ctxFieldsKey))
	if existing, ok := current.(log.Fields); ok {
		for k, v := range fields {
			existing[k] = v
		}
		c.Set(string(ctxFieldsKey), existing)
		return
	}
	c.Set(string(ctxFieldsKey), fields)
}

// OptionsFromEnv builds Options from environment variables.
// Supported variables (with prefix):
//
//	PRETTY (bool), LOG_RESPONSE (bool), LOG_HEADERS (bool), LOG_ON_ERROR_ONLY (bool),
//	MAX_BODY_LOG_SIZE (int), MAX_REQUEST_BODY_BYTES (int64), MAX_RESPONSE_BODY_BYTES (int64),
//	SAMPLE_RATE (float64), REQUEST_ID_HEADER (string), INJECT_REQUEST_ID (bool), STRUCTURED (bool)
func OptionsFromEnv(prefix string) Options {
	if prefix == "" {
		prefix = "REQUEST_LOGGER_"
	}
	opts := defaultOptions()

	opts.Pretty = envBool(prefix+"PRETTY", opts.Pretty)
	opts.LogResponse = envBool(prefix+"LOG_RESPONSE", opts.LogResponse)
	opts.LogHeaders = envBool(prefix+"LOG_HEADERS", opts.LogHeaders)
	opts.LogOnErrorOnly = envBool(prefix+"LOG_ON_ERROR_ONLY", opts.LogOnErrorOnly)
	opts.Structured = envBool(prefix+"STRUCTURED", opts.Structured)
	opts.InjectRequestID = envBool(prefix+"INJECT_REQUEST_ID", opts.InjectRequestID)

	if v := envString(prefix + "REQUEST_ID_HEADER"); v != "" {
		opts.RequestIDHeader = v
	}

	opts.MaxBodyLogSize = envInt(prefix+"MAX_BODY_LOG_SIZE", opts.MaxBodyLogSize)
	opts.MaxRequestBodyBytes = envInt64(prefix+"MAX_REQUEST_BODY_BYTES", opts.MaxRequestBodyBytes)
	opts.MaxResponseBodyBytes = envInt64(prefix+"MAX_RESPONSE_BODY_BYTES", opts.MaxResponseBodyBytes)
	opts.SampleRate = envFloat(prefix+"SAMPLE_RATE", opts.SampleRate)

	if v := envString(prefix + "HEADER_ALLOWLIST"); v != "" {
		opts.HeaderAllowlist = splitCSV(v)
	}
	if v := envString(prefix + "HEADER_DENYLIST"); v != "" {
		opts.HeaderDenylist = splitCSV(v)
	}

	return applyDefaults(opts)
}

func envString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envBool(key string, defaultVal bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func envInt(key string, defaultVal int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func envInt64(key string, defaultVal int64) int64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func envFloat(key string, defaultVal float64) float64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
