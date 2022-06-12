package errs

const (
	BaseError = iota
	AuthError
)

type ApplicationError struct {
	Code int
	Msg  string
}

func (e *ApplicationError) Error() string {
	return e.Msg
}

func NewAuthError(msg string) error {
	return &ApplicationError{
		Code: AuthError,
		Msg:  msg,
	}
}

func IsAuthError(err error) bool {
	if e, ok := err.(*ApplicationError); !ok {
		return false
	} else if e.Code == AuthError {
		return true
	}

	return false
}
