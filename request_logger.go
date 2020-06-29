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

type handler struct {
	isDebug bool
	logger  *log.Logger
}

func New(options Options) gin.HandlerFunc {
	handler := handler{
		isDebug: false,
		logger:  log.New(),
	}

	handler.logger.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "02.01.2006 15:04:05",
	})

	if options.IsDebug {
		handler.isDebug = true
		handler.logger.SetLevel(log.TraceLevel)
	}

	return handler.handle(options)
}

func (h *handler) handle(options Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestId := c.GetHeader("X-Request-ID")
		if requestId == "" {
			requestId = uuid.New().String()
		}

		c.Set(RequestContextUUIDTag, requestId)
		c.Set(RequestContextIPTag, c.ClientIP())

		c.Header("X-Request-ID", requestId)

		requestLogger := h.logger.WithFields(log.Fields{"request_id": requestId, "user_ip": c.ClientIP()})

		var body string

		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			bufs, err := ioutil.ReadAll(c.Request.Body)
			if err != nil {
				log.Error("error while reading request body")
			}
			firstCloser := ioutil.NopCloser(bytes.NewBuffer(bufs))
			secondCloser := ioutil.NopCloser(bytes.NewBuffer(bufs))

			body = readBody(firstCloser)
			c.Request.Body = secondCloser

		} else {
			body = "GET URI: " + c.Request.RequestURI
		}

		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw
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

		if options.LogResponse {
			log.Tracef("[Response] Body: %s\n", blw.body.String())
		}
	}
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w bodyLogWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
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
	requestLogger = setContextDataTologger(c, requestLogger).WithField("response_status", s)

	if len(reqBody) > 0 {
		requestLogger.Errorf("[Server Error] Request Body: %s\n", reqBody)
	} else {
		requestLogger.Error("Server Error response")
	}
}

func handleBadRequest(s int, c *gin.Context, reqBody string, requestLogger *log.Entry) {
	requestLogger = setContextDataTologger(c, requestLogger).WithField("response_status", s)

	if len(reqBody) > 0 {
		requestLogger.Warnf("[Bad Request] Request Body: %s\n", reqBody)
	} else {
		requestLogger.Warn("Bad Request response")
	}
}

func handleNormalResponse(s int, c *gin.Context, reqBody string, requestLogger *log.Entry) {
	requestLogger = setContextDataTologger(c, requestLogger).WithField("response_status", s)

	if len(reqBody) > 0 {
		requestLogger.Tracef("[OK Response] Request Body: %s\n", reqBody)
	} else {
		requestLogger.Trace("Normal response")
	}
}

func setContextDataTologger(c *gin.Context, logger *log.Entry) *log.Entry {
	response, exists := c.Get(ResponseContextBodyTag)
	if exists {
		logger = logger.WithField("response_body", response)
	}
	context, exists := c.Get(ResponseContextInfoTag)
	if exists {
		logger = logger.WithField("response_context", context)
	}

	return logger
}
