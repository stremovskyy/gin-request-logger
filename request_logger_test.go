package gin_request_logger

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test router
	router := gin.New()
	router.Use(RequestLogger(false))

	// Add a test route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Test response"})
	})

	// Create a test request
	req := httptest.NewRequest("GET", "/test?param1=value1&param2=value2", nil)
	req.Header.Set("X-Request-ID", "test-request-id")

	// Create a test response recorder
	w := httptest.NewRecorder()

	// Perform the request
	router.ServeHTTP(w, req)

	// Assert response status code
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
	}

	// Assert response body
	responseBody, err := ioutil.ReadAll(w.Body)
	if err != nil {
		t.Errorf("Failed to read response body: %v", err)
	}
	expectedResponseBody := `{"message":"Test response"}`
	if string(responseBody) != expectedResponseBody {
		t.Errorf("Expected response body %s, but got %s", expectedResponseBody, string(responseBody))
	}

	// Assert log messages
	logOutput := w.Body.String()
	assertLogMessageContains(t, logOutput, "[test-request-id] URL: /test")
	assertLogMessageContains(t, logOutput, "[test-request-id] Parameters: param1=value1, param2=value2")
	assertLogMessageContains(t, logOutput, "[test-request-id] {\"message\":\"Test response\"}")
	assertLogMessageContains(t, logOutput, "[test-request-id] Status: 200 OK")
}

func assertLogMessageContains(t *testing.T, logOutput, message string) {
	if !strings.Contains(logOutput, message) {
		t.Errorf("Expected log output to contain message: %s", message)
	}
}
