// Snippet: internal/cliexit/cliexit.go
//
// Three typed error wrappers used by main.go to map a returned error from
// cmd.Execute to a structured exit code:
//
//	*AuthError       → exit 2  (no token / wrong scope)
//	*ValidationError → exit 3  (input rejected pre-request)
//	*DiscoveryError  → exit 4  (spec/schema problem)
//
// APIError lives in internal/api/ alongside the HTTP client. Its
// StatusCode==401||403 case is also exit 2; otherwise exit 1.
package cliexit

type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

type DiscoveryError struct{ Err error }

func (e *DiscoveryError) Error() string { return e.Err.Error() }
func (e *DiscoveryError) Unwrap() error { return e.Err }
