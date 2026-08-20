package middleware

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const groupsKey ctxKey = 1

// Auth is a placeholder JWT/Cognito verifier. In dev it reads groups from
// the `X-Dev-Groups` header for easy testing. In prod, replace with real
// JWT verification against the Cognito JWKS.
func Auth(dev bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var groups []string
			if dev {
				if h := r.Header.Get("X-Dev-Groups"); h != "" {
					groups = strings.Split(h, ",")
				}
			} else {
				// TODO: verify Bearer JWT, extract cognito:groups
				http.Error(w, "prod auth not wired", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), groupsKey, groups)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GroupsFromContext(ctx context.Context) []string {
	if v := ctx.Value(groupsKey); v != nil {
		if gs, ok := v.([]string); ok {
			return gs
		}
	}
	return nil
}
