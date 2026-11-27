package api

import (
	"context"
	"github.com/TeemoSun/mihomo-deck/internal/database"
	"net/http"
)

func contextWithUser(r *http.Request, u database.User) context.Context {
	return context.WithValue(r.Context(), userKey{}, u)
}
