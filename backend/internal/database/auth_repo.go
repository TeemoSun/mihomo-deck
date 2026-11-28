package database

import (
	"database/sql"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID                     int64
	Username, PasswordHash string
	TokenVersion           int64
}
type RefreshToken struct {
	ID, SessionID            string
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
func UpdatePassword(db *sql.DB, id, expectedVersion int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE users SET password_hash=?,token_version=token_version+1,updated_at=? WHERE id=? AND token_version=?", string(hash), time.Now().Unix(), id, expectedVersion)
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
func CreateRefreshTokenIfVersion(db *sql.DB, t RefreshToken, expectedVersion int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRow("SELECT token_version FROM users WHERE id=?", t.UserID).Scan(&version); err != nil {
		return err
	}
	if version != expectedVersion {
		return sql.ErrNoRows
	}
	if err = insertRefreshToken(tx, t, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func insertRefreshToken(tx *sql.Tx, t RefreshToken, now int64) error {
	_, err := tx.Exec("INSERT INTO refresh_tokens(id,user_id,token_hash,user_agent,ip,expires_at,created_at,session_id) VALUES(?,?,?,?,?,?,?,?)", t.ID, t.UserID, t.TokenHash, t.UserAgent, t.IP, t.ExpiresAt, now, t.SessionID)
	return err
}

func GetRefreshUser(db *sql.DB, hash string, now time.Time) (User, error) {
	var u User
	err := db.QueryRow("SELECT u.id,u.username,u.token_version FROM refresh_tokens t JOIN users u ON u.id=t.user_id WHERE t.token_hash=? AND t.revoked_at IS NULL AND t.expires_at>?", hash, now.Unix()).Scan(&u.ID, &u.Username, &u.TokenVersion)
	return u, err
}

func RotateRefreshToken(db *sql.DB, oldHash string, next RefreshToken, now time.Time) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// A legacy token's row ID becomes its session ID on first rotation.
	err = tx.QueryRow("UPDATE refresh_tokens SET revoked_at=? WHERE token_hash=? AND user_id=? AND revoked_at IS NULL AND expires_at>? RETURNING coalesce(nullif(session_id,''),id)", now.Unix(), oldHash, next.UserID, now.Unix()).Scan(&next.SessionID)
	if err != nil {
		return "", err
	}
	if err = insertRefreshToken(tx, next, now.Unix()); err != nil {
		return "", err
	}
	return next.SessionID, tx.Commit()
}

func RevokeRefreshSession(db *sql.DB, hash string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var userID int64
	var sessionID string
	// A rotated cookie can race with logout, so resolve the session even if the row has been revoked.
	err = tx.QueryRow("SELECT user_id,coalesce(nullif(session_id,''),id) FROM refresh_tokens WHERE token_hash=?", hash).Scan(&userID, &sessionID)
	if err != nil {
		return 0, err
	}
	if err = revokeRefreshSession(tx, userID, sessionID); err != nil {
		return 0, err
	}
	return userID, tx.Commit()
}

func RevokeSessionByID(db *sql.DB, userID int64, sessionID string) error {
	return revokeRefreshSession(db, userID, sessionID)
}

type authExecer interface {
	Exec(string, ...any) (sql.Result, error)
}

func revokeRefreshSession(db authExecer, userID int64, sessionID string) error {
	_, err := db.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL AND (session_id=? OR (session_id='' AND id=?))", time.Now().Unix(), userID, sessionID, sessionID)
	return err
}

func ActiveRefreshSession(db *sql.DB, userID int64, sessionID string, now time.Time) (bool, error) {
	var active bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM refresh_tokens WHERE user_id=? AND session_id=? AND revoked_at IS NULL AND expires_at>?)", userID, sessionID, now.Unix()).Scan(&active)
	return active, err
}

func PruneExpiredRefreshTokens(db *sql.DB, now time.Time) error {
	_, err := db.Exec("DELETE FROM refresh_tokens WHERE expires_at<=?", now.Unix())
	return err
}
