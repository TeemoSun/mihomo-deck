package database

import (
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"time"
)

type User struct {
	ID                     int64
	Username, PasswordHash string
	TokenVersion           int64
}
type RefreshToken struct {
	ID                       string
	UserID                   int64
	TokenHash, UserAgent, IP string
	ExpiresAt                int64
}

func UsersEmpty(db *sql.DB) (bool, error) {
	var n int
	err := db.QueryRow("SELECT count(*) FROM users").Scan(&n)
	return n == 0, err
}
func EnsureAdminSeed(db *sql.DB, username, password string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		now := time.Now().Unix()
		if _, err = tx.Exec("INSERT INTO users(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)", username, string(hash), now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func GetUserByUsername(db *sql.DB, username string) (User, error) {
	var u User
	err := db.QueryRow("SELECT id,username,password_hash,token_version FROM users WHERE username=?", username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TokenVersion)
	return u, err
}
func GetUserByID(db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRow("SELECT id,username,password_hash,token_version FROM users WHERE id=?", id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TokenVersion)
	return u, err
}
func UpdatePassword(db *sql.DB, id int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE users SET password_hash=?,token_version=token_version+1,updated_at=? WHERE id=?", string(hash), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", time.Now().Unix(), id); err != nil {
		return err
	}
	return tx.Commit()
}
func CreateRefreshToken(db *sql.DB, t RefreshToken) error {
	_, err := db.Exec("INSERT INTO refresh_tokens(id,user_id,token_hash,user_agent,ip,expires_at,created_at) VALUES(?,?,?,?,?,?,?)", t.ID, t.UserID, t.TokenHash, t.UserAgent, t.IP, t.ExpiresAt, time.Now().Unix())
	return err
}
func RotateRefreshToken(db *sql.DB, oldHash string, next RefreshToken, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE token_hash=? AND user_id=? AND revoked_at IS NULL AND expires_at>?", now.Unix(), oldHash, next.UserID, now.Unix())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("invalid refresh token")
	}
	if _, err = tx.Exec("INSERT INTO refresh_tokens(id,user_id,token_hash,user_agent,ip,expires_at,created_at) VALUES(?,?,?,?,?,?,?)", next.ID, next.UserID, next.TokenHash, next.UserAgent, next.IP, next.ExpiresAt, now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func RevokeRefreshToken(db *sql.DB, hash string) error {
	_, err := db.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL", time.Now().Unix(), hash)
	return err
}
func RevokeAllUserTokens(db *sql.DB, id int64) error {
	_, err := db.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", time.Now().Unix(), id)
	return err
}
func PruneExpiredRefreshTokens(db *sql.DB, now time.Time) error {
	_, err := db.Exec("DELETE FROM refresh_tokens WHERE expires_at<=?", now.Unix())
	return err
}
