package gateway

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	"github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

type UserGateway interface {
	Get(string) (*types.User, error)
	Create(string, string) (*types.User, error)
	Delete(string) error
	Authenticate(string, string) (string, error)
	Deauthenticate(string) error
	ChangePassword(string, string) (string, error)
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
var getToken = "SELECT value, user_id, expires FROM TOKEN WHERE value = ?"
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
			log.Fatalf("failed to setup userDb table: %s", err)
		}
	}

}

func (s *userGateway) Get(id string) (*types.User, error) {
	row := s.db.QueryRow(getUserById, id)

	if row.Err() != nil {
		return nil, row.Err()
	}

	user := &types.User{}

	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NewAuthError("user does not exist")
		}
		return nil, err
	}

	return user, nil
}

func (s *userGateway) Create(email string, pass string) (*types.User, error) {
	if !strings.Contains(email, "@") {
		return nil, errs.NewAuthError("Invalid email format")
	}

	userID := string(types.NewV4())
	hash, err := HashPassword(pass)

	if err != nil {
		return nil, err
	}

	if _, err = s.db.Exec(createUser, userID, email, hash); err != nil {
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

func (s *userGateway) Authenticate(email string, pass string) (string, error) {
	return "", nil
}
func (s *userGateway) Deauthenticate(email string) error {
	return nil
}
func (s *userGateway) ChangePassword(userID string, password string) (string, error) {
	return "", nil
}

func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}
