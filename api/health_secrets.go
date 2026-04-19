package main

import "os"

// healthSecretsFromEnv returns the list of HMAC secrets the /healthz/detailed
// endpoint will accept. The second value supports a brief rotation window so
// the dashboard can still sign with the previous secret while services pick up
// the new one. Empty values are dropped by the middleware.
func healthSecretsFromEnv() [][]byte {
	return [][]byte{
		[]byte(os.Getenv("HEALTH_AUTH_SECRET")),
		[]byte(os.Getenv("HEALTH_AUTH_SECRET_PREVIOUS")),
	}
}
