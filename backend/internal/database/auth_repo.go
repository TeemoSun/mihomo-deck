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

func SyncAdmin(db *sql.DB, username, password string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var u User
	err = tx.QueryRow("SELECT id,username,password_hash,token_version FROM users ORDER BY id LIMIT 1").Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TokenVersion)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && u.Username == username && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
		return tx.Commit()
	}
	newUser := err == sql.ErrNoRows
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if newUser {
		if _, err = tx.Exec("INSERT INTO users(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)", username, string(hash), now, now); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.Exec("UPDATE users SET username=?,password_hash=?,token_version=token_version+1,updated_at=? WHERE id=?", username, string(hash), now, u.ID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE refresh_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", now, u.ID); err != nil {
		return err
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
func CreateRefreshToken(db *sql.DB, t RefreshToken) error {
	return insertRefreshToken(db, t, time.Now().Unix())
}

func insertRefreshToken(db authExecer, t RefreshToken, now int64) error {
	_, err := db.Exec("INSERT INTO refresh_tokens(id,user_id,token_hash,user_agent,ip,expires_at,created_at,session_id) VALUES(?,?,?,?,?,?,?,?)", t.ID, t.UserID, t.TokenHash, t.UserAgent, t.IP, t.ExpiresAt, now, t.SessionID)
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
