package sql

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	kvPasswordRegex  = regexp.MustCompile(`(?i)(password|pass|secret|token|api_key|apikey)\s*=\s*['"]?([^'"\s&]+)['"]?`)
	uriPasswordRegex = regexp.MustCompile(`://([^:]+):([^@]+)@`)
	mysqlDSNRegex    = regexp.MustCompile(`(?i)(^|[\s"'])([^:@\s"']+):([^@\s"']+)@((?:tcp|unix)\([^)]*\)|/)`)
)

var sensitiveQueryParams = map[string]bool{
	"password":   true,
	"pass":       true,
	"token":      true,
	"auth_token": true,
	"secret":     true,
	"api_key":    true,
	"apikey":     true,
}

// SanitizeDSN intercepts connection strings and redacts sensitive credentials
// (passwords, auth tokens, secrets) to prevent accidental leakage into logs, error traces, or telemetry.
func SanitizeDSN(raw string) string {
	if raw == "" {
		return ""
	}

	result := raw

	// 1. Try URL parsing if URI scheme is present
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
			// Redact userinfo password
			if u.User != nil {
				if _, hasPass := u.User.Password(); hasPass {
					u.User = url.UserPassword(u.User.Username(), "*****")
				}
			}

			// Redact sensitive query parameters
			q := u.Query()
			modifiedQuery := false
			for k := range q {
				lowerKey := strings.ToLower(k)
				if sensitiveQueryParams[lowerKey] {
					q.Set(k, "*****")
					modifiedQuery = true
				}
			}
			if modifiedQuery {
				u.RawQuery = q.Encode()
			}

			result = u.String()
			result = strings.ReplaceAll(result, "%2A%2A%2A%2A%2A", "*****")
		} else {
			// Fallback URL regex masking if url.Parse fails (e.g. malformed host)
			result = uriPasswordRegex.ReplaceAllString(result, "://$1:*****@")
		}
	}

	// 2. Redact Go MySQL driver DSN format credentials (e.g. "user:pass@tcp(...)" or "user:pass@unix(...)")
	result = mysqlDSNRegex.ReplaceAllString(result, "${1}${2}:*****@${4}")

	// 3. Redact key-value format credentials (e.g. "password=secret", "api_key=secret", etc.)
	result = kvPasswordRegex.ReplaceAllString(result, "$1=*****")

	return result
}
