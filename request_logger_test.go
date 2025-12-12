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
		LogRequest:     true,
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
		LogRequest:     true,
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
		LogRequest:     true,
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
		LogRequest:     true,
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

func TestLogOnErrorOnlySkipsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		LogRequest:     true,
		LogResponse:    true,
		LogOnErrorOnly: true,
	}))

	router.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/fail", func(c *gin.Context) {
		c.String(http.StatusInternalServerError, "fail")
	})

	reqOK := httptest.NewRequest("GET", "/ok", nil)
	reqOK.Header.Set("X-Request-ID", "ok-id")
	okLogs := captureLogs(t, func() {
		router.ServeHTTP(httptest.NewRecorder(), reqOK)
	})

	if strings.TrimSpace(okLogs) != "" {
		t.Fatalf("expected no logs for success when LogOnErrorOnly is set, got: %s", okLogs)
	}

	reqFail := httptest.NewRequest("GET", "/fail", nil)
	reqFail.Header.Set("X-Request-ID", "fail-id")
	failLogs := captureLogs(t, func() {
		router.ServeHTTP(httptest.NewRecorder(), reqFail)
	})

	if !strings.Contains(failLogs, "[fail-id] Status: 500 Internal Server Error") {
		t.Fatalf("expected error logs, got: %s", failLogs)
	}
}

func TestHeaderAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		LogRequest:      true,
		LogResponse:     true,
		LogHeaders:      true,
		HeaderAllowlist: []string{"X-Allowed", "X-Allowed-Resp"},
	}))

	router.GET("/headers", func(c *gin.Context) {
		c.Header("X-Allowed-Resp", "resp-ok")
		c.Header("X-Blocked-Resp", "nope")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/headers", nil)
	req.Header.Set("X-Allowed", "yes")
	req.Header.Set("X-Blocked", "no")

	logs := captureLogs(t, func() {
		router.ServeHTTP(httptest.NewRecorder(), req)
	})

	if strings.Contains(logs, "X-Blocked") || strings.Contains(logs, "X-Blocked-Resp") {
		t.Fatalf("blocked headers should not be logged, got: %s", logs)
	}
	if !strings.Contains(logs, "X-Allowed=yes") || !strings.Contains(logs, "X-Allowed-Resp=resp-ok") {
		t.Fatalf("allowed headers should be logged, got: %s", logs)
	}
}

func TestSampleRateZeroSkipsLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		LogRequest:    true,
		LogResponse:   true,
		SampleRate:    0,
		SampleRateSet: true,
	}))

	router.GET("/sample", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/sample", nil)
	req.Header.Set("X-Request-ID", "sample-id")

	logs := captureLogs(t, func() {
		router.ServeHTTP(httptest.NewRecorder(), req)
	})

	if strings.TrimSpace(logs) != "" {
		t.Fatalf("expected no logs when sample rate is zero, got: %s", logs)
	}
}

func TestPerRequestLimitOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestLoggerWithOptions(Options{
		LogRequest:     true,
		LogResponse:    true,
		MaxBodyLogSize: 100,
	}))

	router.POST("/limit", func(c *gin.Context) {
		SetBodyLogLimit(c, 10)
		c.String(http.StatusOK, strings.Repeat("r", 30))
	})

	req := httptest.NewRequest("POST", "/limit", strings.NewReader(strings.Repeat("q", 30)))
	req.Header.Set("X-Request-ID", "limit-id")

	logs := captureLogs(t, func() {
		router.ServeHTTP(httptest.NewRecorder(), req)
	})

	if !strings.Contains(logs, "... [truncated to 10 bytes]") {
		t.Fatalf("expected body to be truncated to override limit, got: %s", logs)
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
