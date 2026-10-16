package utils

import "os"

// GetEnv returns the environment variable value, or def when it is unset or empty.
func GetEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
