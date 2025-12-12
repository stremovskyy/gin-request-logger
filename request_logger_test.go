package gin_request_logger

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func TestResponseLoggingGunzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		Pretty:         false,
		LogResponse:    true,
		MaxBodyLogSize: 1024,
	}))

	router.GET("/gzip", func(c *gin.Context) {
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		_, _ = writer.Write([]byte(`{"message":"hello"}`))
		writer.Close()

		c.Header("Content-Encoding", "gzip")
		c.Data(http.StatusOK, "application/json", buf.Bytes())
	})

	req := httptest.NewRequest("GET", "/gzip", nil)
	req.Header.Set("X-Request-ID", "test-gzip")
	w := httptest.NewRecorder()

	logs := captureLogs(t, func() {
		router.ServeHTTP(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if !strings.Contains(logs, `[test-gzip] {\"message\":\"hello\"}`) {
		t.Fatalf("expected gunzipped response body in logs, got: %s", logs)
	}
}

func TestBinaryResponseOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		Pretty:         false,
		LogResponse:    true,
		MaxBodyLogSize: 1024,
	}))

	router.GET("/bin", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/octet-stream", []byte{0x01, 0x02, 0x03, 0x04, 0xff})
	})

	req := httptest.NewRequest("GET", "/bin", nil)
	req.Header.Set("X-Request-ID", "test-bin")
	w := httptest.NewRecorder()

	logs := captureLogs(t, func() {
		router.ServeHTTP(w, req)
	})

	if !strings.Contains(logs, "[binary content omitted") {
		t.Fatalf("expected binary placeholder in logs, got: %s", logs)
	}
	if strings.Contains(logs, "\u0001\u0002\u0003") {
		t.Fatalf("expected raw binary not to appear in logs")
	}
}

func TestMultipartRequestOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		Pretty:         false,
		LogResponse:    true,
		MaxBodyLogSize: 1024,
	}))

	router.POST("/upload", func(c *gin.Context) {
		c.String(http.StatusCreated, "ok")
	})

	body := strings.NewReader("--abc\r\nContent-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\nContent-Type: text/plain\r\n\r\nhello\r\n--abc--")
	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	req.Header.Set("X-Request-ID", "test-multipart")

	w := httptest.NewRecorder()

	logs := captureLogs(t, func() {
		router.ServeHTTP(w, req)
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	if !strings.Contains(logs, "[binary content omitted") {
		t.Fatalf("expected multipart placeholder in logs, got: %s", logs)
	}
}

func TestBodyTruncation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := 50
	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		Pretty:         false,
		LogResponse:    true,
		MaxBodyLogSize: limit,
	}))

	longResponse := strings.Repeat("a", 200)
	longRequest := strings.Repeat("b", 200)

	router.POST("/truncate", func(c *gin.Context) {
		c.String(http.StatusOK, longResponse)
	})

	req := httptest.NewRequest("POST", "/truncate", strings.NewReader(longRequest))
	req.Header.Set("X-Request-ID", "test-truncate")
	w := httptest.NewRecorder()

	logs := captureLogs(t, func() {
		router.ServeHTTP(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	expectedSuffix := fmt.Sprintf("... [truncated to %d bytes]", limit)
	if strings.Count(logs, expectedSuffix) < 2 {
		t.Fatalf("expected truncated indicator to appear for request and response bodies, got logs: %s", logs)
	}

	if !strings.Contains(logs, strings.Repeat("a", limit)+expectedSuffix) {
		t.Fatalf("expected truncated response body in logs, got: %s", logs)
	}

	if !strings.Contains(logs, strings.Repeat("b", limit)+expectedSuffix) {
		t.Fatalf("expected truncated request body in logs, got: %s", logs)
	}
}

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer

	logger := log.StandardLogger()
	prevOut := logger.Out
	prevLevel := logger.GetLevel()

	logger.SetOutput(&buf)
	logger.SetLevel(log.TraceLevel)

	fn()

	logger.SetOutput(prevOut)
	logger.SetLevel(prevLevel)

	return buf.String()
}
