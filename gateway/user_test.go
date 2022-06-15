package gateway

import (
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/stretchr/testify/assert"
)

func TestCreateUserExists(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "test@test.com"
	password := "test123"
	_, err := gateway.Create(email, password)

	assert.NoError(t, err)

	_, err = gateway.Create(email, password)

	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestCreateAndGet(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"

	user, err := gateway.Create(email, password)

	assert.NoError(t, err)
	assert.Equal(t, user.Email, email)

	gotUser, err := gateway.Get(user.ID)

	assert.NoError(t, err)
	assert.EqualValues(t, user, gotUser)
}

func TestGetUserDoesntExist(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	userID := string(types.NewV4())
	user, err := gateway.Get(userID)

	assert.Nil(t, user)
	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestDelete(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)
	assert.NoError(t, gateway.Delete(user.ID))

	u, err := gateway.Get(user.ID)

	assert.Nil(t, u)
	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestDeleteUserDoesntExist(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	err := gateway.Delete(string(types.NewV4()))

	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestAuthenticate(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, password)

	assert.NoError(t, err)
	assert.Equal(t, token.UserID, user.ID)
}

func TestAuthenticateNoUser(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"

	token, err := gateway.Authenticate(email, password)

	assert.Error(t, err)
	assert.Nil(t, token)
}

func TestAuthenticateIncorrectPassword(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	wrongPassword := "notest"
	_, err := gateway.Create(email, password)

	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, wrongPassword)

	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
	assert.Nil(t, token)
}

func TestIsNotAuthenticatedAfterDeauthenticate(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, password)

	assert.NoError(t, err)
	assert.Equal(t, token.UserID, user.ID)
	assert.NoError(t, gateway.IsAuthenticated(user.ID, token.Value))
	assert.NoError(t, gateway.Deauthenticate(token.Value))
	assert.Error(t, gateway.IsAuthenticated(user.ID, token.Value))
}

func TestIsNotAuthenticatedWrongUserID(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, password)

	assert.NoError(t, err)
	assert.Equal(t, token.UserID, user.ID)

	err = gateway.IsAuthenticated(string(types.NewV4()), token.Value)

	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestIsNotAuthenticatedWrongToken(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, password)

	assert.NoError(t, err)
	assert.Equal(t, token.UserID, user.ID)

	err = gateway.IsAuthenticated(user.ID, "invalid-token")

	assert.Equal(t, errs.AuthError, err.(*errs.ApplicationError).Code)
}

func TestChangePassword(t *testing.T) {
	gateway := NewUserGateway(":memory:")
	email := "user@email.com"
	password := "test1234"
	newPassword := "test1"
	user, err := gateway.Create(email, password)

	assert.NoError(t, err)

	to, err := gateway.ChangePassword(user.ID, newPassword)

	assert.NotNil(t, to)
	assert.NoError(t, err)

	token, err := gateway.Authenticate(email, newPassword)

	assert.NoError(t, err)
	assert.Equal(t, token.UserID, user.ID)

	err = gateway.IsAuthenticated(user.ID, token.Value)

	assert.NoError(t, err)
}
