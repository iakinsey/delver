package gateway

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

const tokenSize = 128
const tokenExpiration = (24 * time.Hour) * 365 // 1 year

type UserGateway interface {
	Get(string) (*types.User, error)
	Create(string, string) (*types.User, error)
	Delete(string) error
	Authenticate(string, string) (*types.Token, error)
	Deauthenticate(string) error
	IsAuthenticated(string, string) error
	ChangePassword(string, string) (*types.Token, error)
}

type userGateway struct {
	db *sql.DB
}

var createUserDb = `
	CREATE TABLE IF NOT EXISTS user (
		id STRING PRIMARY KEY,
		email STRING NOT NULL UNIQUE,
		password_hash BLOB NOT NULL
	)
`

var createTokenDb = `
	CREATE TABLE IF NOT EXISTS token (
		value STRING PRIMARY KEY,
		user_id STRING NOT NULL,
		expires INTEGER NOT NULL
	)
`

var getUserById = "SELECT id, email, password_hash FROM user WHERE id = ?"
var getUserByEmail = "SELECT id, email, password_hash FROM USER where email = ?"
var createUser = "INSERT INTO user (id, email, password_hash) VALUES (?, ?, ?)"
var changePassword = "UPDATE user SET password_hash = ? WHERE id = ?"
var deleteUser = "DELETE FROM user WHERE id = ?"
var setToken = "INSERT INTO TOKEN (value, user_id, expires) VALUES (?, ?, ?)"
var getToken = "SELECT value, user_id, expires FROM TOKEN WHERE user_id = ? AND value = ?"
var deleteToken = "DELETE FROM token WHERE value = ?"
var clearTokens = "DELETE FROM token WHERE user_id = ?"

func NewUserGateway(path string) UserGateway {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s", path))

	if err != nil {
		log.Fatalf("failed to open user sqlite database %s", err)
	}

	g := &userGateway{
		db: db,
	}

	g.setupUserDb()

	return g
}

func (s *userGateway) setupUserDb() {
	d := []string{
		createUserDb,
		createTokenDb,
	}

	for _, q := range d {
		if _, err := s.db.Exec(q); err != nil {
			log.Fatalf("failed to setup user database table: %s", err)
		}
	}

}

func (s *userGateway) Get(id string) (*types.User, error) {
	return s.getBy(getUserById, id)
}

func (s *userGateway) Create(email, pass string) (*types.User, error) {
	if !strings.Contains(email, "@") {
		return nil, errs.NewAuthError("Invalid email format")
	}

	userID := string(types.NewV4())
	hash := HashPassword(pass)

	if _, err := s.db.Exec(createUser, userID, email, hash); err != nil {
		if e, ok := err.(sqlite3.Error); ok && e.Code == sqlite3.ErrConstraint {
			return nil, errs.NewAuthError("User exists")
		}

		return nil, err
	}

	return &types.User{
		ID:           userID,
		Email:        email,
		PasswordHash: hash,
	}, nil
}

func (s *userGateway) Delete(userID string) error {
	if u, err := s.Get(userID); err != nil && !errs.IsAuthError(err) {
		return err
	} else if u == nil {
		return errs.NewAuthError("User doesnt exist")
	}

	_, err := s.db.Exec(deleteUser, userID)

	return err
}

func (s *userGateway) Authenticate(email, pass string) (*types.Token, error) {
	user, err := s.getBy(getUserByEmail, email)

	if err != nil {
		return nil, err
	}

	if err := CheckPassword(pass, user.PasswordHash); err != nil {
		return nil, err
	}

	return s.setUserToken(user.ID)
}
func (s *userGateway) Deauthenticate(value string) error {
	_, err := s.db.Exec(deleteToken, value)

	return err
}

func (s *userGateway) IsAuthenticated(userId, tokenValue string) error {
	t, err := s.getToken(userId, tokenValue)

	if err != nil {
		return err
	}

	if t.Expires.After(time.Now()) {
		return nil
	}

	if err := s.Deauthenticate(tokenValue); err != nil {
		return err
	}

	return errs.NewAuthError("Session expired")
}

func (s *userGateway) ChangePassword(userID, pass string) (*types.Token, error) {
	_, err := s.getBy(getUserById, userID)

	if err != nil {
		return nil, err
	}

	hash := HashPassword(pass)

	if _, err := s.db.Exec(changePassword, hash, userID); err != nil {
		return nil, err
	}

	if err := s.purgeTokens(userID); err != nil {
		return nil, err
	}

	return s.setUserToken(userID)
}

func (s *userGateway) getBy(query, key string) (*types.User, error) {
	row := s.db.QueryRow(query, key)

	if row.Err() != nil {
		return nil, row.Err()
	}

	user := &types.User{}

	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NewAuthError("User does not exist")
		}
		return nil, err
	}

	return user, nil
}

func (s *userGateway) getToken(userID, tokenValue string) (*types.Token, error) {
	row := s.db.QueryRow(getToken, userID, tokenValue)

	if row.Err() != nil {
		return nil, row.Err()
	}

	token := &types.Token{}
	var expires int64

	if err := row.Scan(&token.Value, &token.UserID, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NewAuthError("Unauthorized")
		}

		return nil, err
	}

	token.Expires = time.Unix(expires, 0)

	return token, nil
}

func (s *userGateway) setUserToken(userId string) (*types.Token, error) {
	token := CreateToken()
	expires := time.Now().Add(tokenExpiration)

	if _, err := s.db.Exec(setToken, token, userId, expires.Unix()); err != nil {
		return nil, err
	}

	return &types.Token{
		UserID:  userId,
		Expires: expires,
		Value:   token,
	}, nil
}

func (s *userGateway) purgeTokens(userID string) error {
	_, err := s.db.Exec(clearTokens, userID)

	return err
}

func HashPassword(password string) []byte {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	if err != nil {
		log.Panicf("unable to hash password %s", err)
	}

	return b
}

func CheckPassword(password string, hash []byte) error {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))

	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return errs.NewAuthError("Incorrect password")
	}

	return err
}

func CreateToken() string {
	b := make([]byte, tokenSize)

	if _, err := rand.Read(b); err != nil {
		log.Panicf("unable to read from random buffer %s", err)
	}

	return hex.EncodeToString(b)
}
