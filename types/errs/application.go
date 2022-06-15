package errs

const (
	BaseError = iota
	AuthError
	DashError
)

type ApplicationError struct {
	Code int
	Msg  string
}

func (e *ApplicationError) Error() string {
	return e.Msg
}

func IsApplicationError(code int, err error) bool {
	if err == nil {
		return false
	} else if e, ok := err.(*ApplicationError); !ok {
		return false
	} else if e.Code == code {
		return true
	}

	return false

}

func NewAuthError(msg string) error {
	return &ApplicationError{
		Code: AuthError,
		Msg:  msg,
	}
}

func NewDashError(msg string) error {
	return &ApplicationError{
		Code: DashError,
		Msg:  msg,
	}
}

func IsAuthError(err error) bool {
	return IsApplicationError(AuthError, err)
}

func IsDashError(err error) bool {
	return IsApplicationError(DashError, err)
}
