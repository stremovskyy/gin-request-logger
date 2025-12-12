package gin_request_logger

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var (
	requestCounter uint64     // Counter for generating unique request IDs
	mutex          sync.Mutex // Mutex for thread-safe counter increment
)

const (
	// ANSI color escape sequences
	ColorReset  = "\033[0m"
	ColorCyan   = "\033[36m"
	ColorYellow = "\033[33m"
	ColorGreen  = "\033[32m"
	ColorRed    = "\033[31m"

	defaultMaxBodyLogSize = 1024 // 1 KB
)

func RequestLogger(pretty bool) gin.HandlerFunc {
	return RequestLoggerWithOptions(Options{
		Pretty:         pretty,
		LogResponse:    true,
		MaxBodyLogSize: defaultMaxBodyLogSize,
	})
}

func RequestLoggerWithOptions(options Options) gin.HandlerFunc {
	opts := applyDefaults(options)

	return func(c *gin.Context) {
		requestID := getRequestID(c)

		requestBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
			return
		}

		// Restore the request body to the original state
		c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))

		log.Trace(ColorCyan + "╔═════HTTP REQUEST\n")
		log.Trace(fmt.Sprintf("[%s] URL: %s", requestID, c.Request.URL.String()))
		log.Trace(fmt.Sprintf("[%s] Parameters: %s", requestID, getURLParameters(c.Request.URL)))
		log.Trace(ColorCyan + "╚═════END HTTP REQUEST" + ColorReset)

		log.Trace(ColorCyan + "╔═════HTTP REQUEST BODY\n")
		log.Trace(fmt.Sprintf("[%s] %s", requestID, prepareBodyForLog(requestBody, c.Request.Header.Get("Content-Type"), opts.Pretty, opts.MaxBodyLogSize)))
		log.Trace(ColorCyan + "╚═════END HTTP REQUEST BODY" + ColorReset)

		body := &bytes.Buffer{}
		writer := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           body,
		}
		c.Writer = writer

		c.Next()

		statusColor := getStatusColor(c.Writer.Status())

		if opts.LogResponse {
			log.Trace(ColorYellow + "╔═════HTTP RESPONSE\n")
			loggedBody := formatResponseBodyForLog(c.Writer.Header(), body.Bytes(), opts.Pretty, opts.MaxBodyLogSize)
			log.Trace(fmt.Sprintf("[%s] %s", requestID, loggedBody))
			log.Trace(ColorYellow + "╚═════END HTTP RESPONSE" + ColorReset)
		}

		log.Trace(statusColor + "╔═════HTTP RESPONSE STATUS\n")
		log.Trace(fmt.Sprintf("[%s] Status: %d %s", requestID, c.Writer.Status(), http.StatusText(c.Writer.Status())))
		log.Trace(statusColor + "╚═════END HTTP RESPONSE STATUS" + ColorReset)
	}
}

func applyDefaults(opts Options) Options {
	if opts.MaxBodyLogSize == 0 {
		opts.MaxBodyLogSize = defaultMaxBodyLogSize
	}

	return opts
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
	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
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

func getRequestID(c *gin.Context) string {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		mutex.Lock()
		defer mutex.Unlock()

		requestCounter++
		requestID = fmt.Sprintf("REQ-%d", requestCounter)
	}

	return requestID
}

func formatResponseBodyForLog(headers http.Header, body []byte, pretty bool, limit int) string {
	contentType := headers.Get("Content-Type")

	decodedBody := body

	if isGzipEncoding(headers.Get("Content-Encoding")) {
		decoded, err := gunzip(body)
		if err == nil {
			decodedBody = decoded
		} else {
			log.Debugf("failed to gunzip response body for logging: %v", err)
		}
	}

	return prepareBodyForLog(decodedBody, contentType, pretty, limit)
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
		strings.Contains(ct, "ms-"):
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
