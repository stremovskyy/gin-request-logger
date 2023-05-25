package gin_request_logger

// Options is a struct for configuring the request logger middleware.
type Options struct {

	// IsDebug is a flag for enabling debug mode.
	IsDebug bool

	// LogResponse is a flag for enabling response logging.
	LogResponse bool

	// Pretty is a flag for enabling pretty JSON logging.
	Pretty bool
}
