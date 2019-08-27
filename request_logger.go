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
		c.Set(RequestContextUUIDTag, uuid.New())

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

			s := c.Writer.Status()

			switch {
			case s >= http.StatusOK && s < http.StatusMultipleChoices:
				handleNormalResponse(s, c, body)
			case s >= http.StatusBadRequest && s < http.StatusInternalServerError:
				handleBadRequest(s, c, body)
			case s >= http.StatusInternalServerError:
				handleServerError(s, c, body)
			default:
				log.Errorf("WTF ERROR!: Status: %d, IP: %12v, Body: %s", s, c.ClientIP(), body)
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

func handleServerError(s int, c *gin.Context, reqBody string) {
	id, exists := c.Get(RequestContextUUIDTag)
	unq := uuid.New()
	if exists {
		unq = id.(uuid.UUID)
	}

	log.WithFields(log.Fields{
		"Status": s,
		"IP":     c.ClientIP(),
		"ID":     unq.String(),
	}).Error("Server Error response")

	if len(reqBody) > 0 {
		log.Infof("Request Body: %s\n", reqBody)
	}
}

func handleBadRequest(s int, c *gin.Context, reqBody string) {
	id, exists := c.Get(RequestContextUUIDTag)
	unq := uuid.New()
	if exists {
		unq = id.(uuid.UUID)
	}

	log.WithFields(log.Fields{
		"Status": s,
		"IP":     c.ClientIP(),
		"ID":     unq.String(),
	}).Warn("Bad Request response")

	if len(reqBody) > 0 {
		log.Infof("Request Body: %s\n", reqBody)
	}
}

func handleNormalResponse(s int, c *gin.Context, reqBody string) {
	id, exists := c.Get(RequestContextUUIDTag)
	unq := uuid.New()
	if exists {
		unq = id.(uuid.UUID)
	}

	log.WithFields(log.Fields{
		"Status": s,
		"IP":     c.ClientIP(),
		"ID":     unq.String(),
	}).Trace("Bad Request response")

	if len(reqBody) > 0 {
		log.Trace("Request Body: %s\n", reqBody)
	}
}
