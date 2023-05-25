package gin_request_logger

import (
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
	ColorReset   = "\033[0m"
	ColorCyan    = "\033[36m"
	ColorYellow  = "\033[33m"
	ColorGreen   = "\033[32m"
	ColorRed     = "\033[31m"
	ColorMagenta = "\033[35m"
)

func RequestLogger(pretty bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Generate and assign a unique request ID
		requestID := generateRequestID()

		// Read the request body
		requestBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
			return
		}

		// Restore the request body to the original state
		c.Request.Body = io.NopCloser(strings.NewReader(string(requestBody)))

		// Log the request URL, parameters, and request ID
		log.Trace(ColorCyan + "╔═════HTTP REQUEST\n")
		log.Trace(fmt.Sprintf("[%s] URL: %s", requestID, c.Request.URL.String()))
		log.Trace(fmt.Sprintf("[%s] Parameters: %s", requestID, getURLParameters(c.Request.URL)))
		log.Trace(ColorCyan + "╚═════END HTTP REQUEST" + ColorReset)

		// Log the request body as a nicely formatted JSON with cyan color
		log.Trace(ColorCyan + "╔═════HTTP REQUEST BODY\n")
		log.Trace(fmt.Sprintf("[%s] %s", requestID, printData(string(requestBody), pretty)))
		log.Trace(ColorCyan + "╚═════END HTTP REQUEST BODY" + ColorReset)

		// Create a custom response writer
		body := &strings.Builder{}
		writer := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           body,
		}
		c.Writer = writer

		// Process the request
		c.Next()

		// Read the response body
		responseBody := body.String()

		// Log the response body as a nicely formatted JSON with yellow color
		log.Trace(ColorYellow + "╔═════HTTP RESPONSE\n")
		log.Trace(fmt.Sprintf("[%s] %s", requestID, printData(responseBody, pretty)))
		log.Trace(ColorYellow + "╚═════END HTTP RESPONSE" + ColorReset)

		// Log the status code with appropriate color
		statusColor := getStatusColor(c.Writer.Status())
		log.Trace(statusColor + "╔═════HTTP RESPONSE STATUS\n")
		log.Trace(fmt.Sprintf("[%s] Status: %d %s", requestID, c.Writer.Status(), http.StatusText(c.Writer.Status())))
		log.Trace(statusColor + "╚═════END HTTP RESPONSE STATUS" + ColorReset)
	}
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
	body *strings.Builder
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

func generateRequestID() string {
	mutex.Lock()
	defer mutex.Unlock()

	requestCounter++
	return fmt.Sprintf("REQ-%d", requestCounter)
}
