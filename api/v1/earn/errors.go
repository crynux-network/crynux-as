package earn

type conflictError struct{ message string }

func (e *conflictError) Error() string          { return e.message }
func (e *conflictError) GetErrorType() string   { return e.message }
func (e *conflictError) SetErrorType(string)    {}
func (e *conflictError) GetMessage() string     { return e.message }
func (e *conflictError) SetMessage(string)      {}
func (e *conflictError) StatusCode() int        { return 409 }

type paymentRequiredError struct{ message string }

func (e *paymentRequiredError) Error() string        { return e.message }
func (e *paymentRequiredError) GetErrorType() string { return e.message }
func (e *paymentRequiredError) SetErrorType(string)  {}
func (e *paymentRequiredError) GetMessage() string   { return e.message }
func (e *paymentRequiredError) SetMessage(string)    {}
func (e *paymentRequiredError) StatusCode() int      { return 402 }

type rateLimitError struct{ message string }

func (e *rateLimitError) Error() string        { return e.message }
func (e *rateLimitError) GetErrorType() string { return e.message }
func (e *rateLimitError) SetErrorType(string)  {}
func (e *rateLimitError) GetMessage() string   { return e.message }
func (e *rateLimitError) SetMessage(string)    {}
func (e *rateLimitError) StatusCode() int      { return 429 }

type serviceUnavailableError struct{ message string }

func (e *serviceUnavailableError) Error() string        { return e.message }
func (e *serviceUnavailableError) GetErrorType() string { return e.message }
func (e *serviceUnavailableError) SetErrorType(string)  {}
func (e *serviceUnavailableError) GetMessage() string   { return e.message }
func (e *serviceUnavailableError) SetMessage(string)    {}
func (e *serviceUnavailableError) StatusCode() int      { return 503 }
