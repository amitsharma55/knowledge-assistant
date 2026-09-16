package middleware

import "net/http"

// RequireAdmin gates the review endpoints. For the demo, admins is an env
// allowlist (KA_ADMIN_USERS) checked against the X-Dev-User identity that Auth
// resolves; setting KA_ADMIN_USERS=dev@example.com makes the default dev user
// the demo admin. Prod replaces this with a Cognito/Entra group check.
func RequireAdmin(admins []string) func(http.Handler) http.Handler {
	set := make(map[string]bool, len(admins))
	for _, a := range admins {
		set[a] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !set[UserFromContext(r.Context())] {
				http.Error(w, "admin access required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
