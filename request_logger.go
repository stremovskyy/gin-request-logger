package gin_request_logger

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestId := uuid.New()
		c.Set(RequestContextUUIDTag, requestId)
		requestLogger := log.WithFields(log.Fields{"request_id": requestId, "user_ip": c.ClientIP()})

		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			bufs, err := ioutil.ReadAll(c.Request.Body)
			if err != nil {
				log.Error("error while reading request body")
			}
			firstCloser := ioutil.NopCloser(bytes.NewBuffer(bufs))
			secondCloser := ioutil.NopCloser(bytes.NewBuffer(bufs))

			body := readBody(firstCloser)
			c.Request.Body = secondCloser
			c.Next()

			status := c.Writer.Status()

			switch {
			case status >= http.StatusOK && status < http.StatusMultipleChoices:
				handleNormalResponse(status, c, body, requestLogger)
			case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
				handleBadRequest(status, c, body, requestLogger)
			case status >= http.StatusInternalServerError:
				handleServerError(status, c, body, requestLogger)
			default:
				log.Errorf("WTF ERROR!: Status: %d, IP: %12v, Body: %status", status, c.ClientIP(), body)
			}
		}
	}
}

func readBody(reader io.Reader) string {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(reader)
	if err != nil {
		log.Error(err)
	}

	s := buf.String()
	return s
}

func handleServerError(s int, c *gin.Context, reqBody string, requestLogger *log.Entry) {
	requestLogger.WithFields(log.Fields{
		"response_status": s,
	}).Error("Server Error response")

	if len(reqBody) > 0 {
		log.Infof("Request Body: %s\n", reqBody)
	}
}

func handleBadRequest(s int, c *gin.Context, reqBody string, requestLogger *log.Entry) {
	log.WithFields(log.Fields{
		"response_status": s,
	}).Warn("Bad Request response")

	if len(reqBody) > 0 {
		log.Infof("Request Body: %s\n", reqBody)
	}
}

func handleNormalResponse(s int, c *gin.Context, reqBody string, requestLogger *log.Entry) {
	log.WithFields(log.Fields{
		"response_status": s,
	}).Trace("Normal response")

	if len(reqBody) > 0 {
		log.Trace("Request Body: %s\n", reqBody)
	}
}
