package trigger

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthMethod is the authentication method used for a request.
type AuthMethod string

const (
	// AuthMethodNone means no valid authentication method was presented.
	AuthMethodNone AuthMethod = "none"
	// AuthMethodAPIKey means the request authenticated with a Bearer API key.
	AuthMethodAPIKey AuthMethod = "api_key"
	// AuthMethodMTLS means the request authenticated with an mTLS identity.
	AuthMethodMTLS AuthMethod = "mtls"
)

// CallerID is the stable caller identity, for example "api_key:<id>" or
// "spiffe:<subject>".
type CallerID string

// Authenticator checks credentials and returns the caller ID and method.
type Authenticator interface {
	Authenticate(context.Context) (CallerID, AuthMethod, error)
}

// APIKeyAuthenticator authenticates via Bearer token API keys.
type APIKeyAuthenticator struct {
	// keys maps raw API key string to key metadata.
	// In production this is backed by the key store (T02).
	// For T01, a simple map is sufficient.
	keys map[string]*APIKeyMeta
}

// APIKeyMeta holds metadata for an API key.
type APIKeyMeta struct {
	ID      string
	KeyHash string
	Scopes  []string
	Revoked bool
}

// NewAPIKeyAuthenticator creates an authenticator with the given keys.
func NewAPIKeyAuthenticator(keys map[string]*APIKeyMeta) *APIKeyAuthenticator {
	if keys == nil {
		keys = make(map[string]*APIKeyMeta)
	}
	return &APIKeyAuthenticator{keys: keys}
}

// Authenticate extracts a Bearer token from gRPC metadata or HTTP context and
// validates it against the registered keys.
func (a *APIKeyAuthenticator) Authenticate(requestContext context.Context) (CallerID, AuthMethod, error) {
	token := extractBearerToken(requestContext)
	if token == "" {
		return "", AuthMethodNone, status.Error(codes.Unauthenticated, "missing API key")
	}

	meta := a.lookup(token)
	if meta == nil || meta.Revoked {
		return "", AuthMethodNone, status.Error(codes.Unauthenticated, "invalid API key")
	}
	return CallerID("api_key:" + meta.ID), AuthMethodAPIKey, nil
}

func (a *APIKeyAuthenticator) configuredKeys() int {
	if a == nil {
		return 0
	}
	count := 0
	for _, meta := range a.keys {
		if meta != nil && !meta.Revoked {
			count++
		}
	}
	return count
}

func (a *APIKeyAuthenticator) lookup(token string) *APIKeyMeta {
	if a == nil {
		return nil
	}
	var matched *APIKeyMeta
	for key, meta := range a.keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
			matched = meta
		}
	}
	return matched
}

// extractBearerToken gets the Bearer token from gRPC metadata or HTTP context.
func extractBearerToken(requestContext context.Context) string {
	if md, ok := metadata.FromIncomingContext(requestContext); ok {
		vals := md.Get("authorization")
		if len(vals) > 0 {
			if token := bearerToken(vals[0]); token != "" {
				return token
			}
		}
	}
	if v := requestContext.Value(authTokenKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

type authTokenKey struct{}

// WithAuthToken stores the bearer token in context for HTTP/gateway requests.
func WithAuthToken(parent context.Context, token string) context.Context {
	return context.WithValue(parent, authTokenKey{}, token)
}

// AuthInterceptor returns a gRPC unary interceptor that requires authentication.
func AuthInterceptor(auth Authenticator) grpc.UnaryServerInterceptor {
	return func(requestContext context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) { // intentionally ignored (reviewed)
		caller, method, err := auth.Authenticate(requestContext)
		if err != nil {
			return nil, fmt.Errorf("auth interceptor: %w", err)
		}
		requestContext = WithCaller(requestContext, caller, method)
		return handler(requestContext, req)
	}
}

// AuthStreamInterceptor returns a gRPC stream interceptor that requires
// authentication.
func AuthStreamInterceptor(auth Authenticator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error { // intentionally ignored (reviewed)
		caller, method, err := auth.Authenticate(ss.Context())
		if err != nil {
			return fmt.Errorf("auth stream interceptor: %w", err)
		}
		requestContext := WithCaller(ss.Context(), caller, method)
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: requestContext})
	}
}

type callerKey struct{}
type callerMethodKey struct{}

// WithCaller stores the authenticated caller ID and method in context.
func WithCaller(parent context.Context, caller CallerID, method AuthMethod) context.Context {
	parent = context.WithValue(parent, callerKey{}, caller)
	parent = context.WithValue(parent, callerMethodKey{}, method)
	return parent
}

// CallerFromContext returns the authenticated caller ID, if any.
func CallerFromContext(parent context.Context) (CallerID, bool) {
	c, ok := parent.Value(callerKey{}).(CallerID)
	return c, ok
}

// CallerMethodFromContext returns the auth method, if any.
func CallerMethodFromContext(parent context.Context) (AuthMethod, bool) {
	m, ok := parent.Value(callerMethodKey{}).(AuthMethod)
	return m, ok
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

// wrappedStream.Context context.
func (w *wrappedStream) Context() context.Context { return w.ctx }

// CORSMiddleware wraps an HTTP handler with deny-by-default CORS.
type CORSMiddleware struct {
	// allowedOrigins is the set of origins allowed to make cross-origin
	// requests. Empty means deny all.
	allowedOrigins map[string]bool
}

// NewCORSMiddleware creates a middleware with the given allowed origins.
func NewCORSMiddleware(allowedOrigins []string) *CORSMiddleware {
	m := &CORSMiddleware{allowedOrigins: make(map[string]bool)}
	for _, origin := range allowedOrigins {
		m.allowedOrigins[origin] = true
	}
	return m
}

// Wrap wraps the given handler with CORS handling.
func (c *CORSMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		originAllowed := origin != "" && c.allowedOrigins[origin]
		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			if origin != "" && !originAllowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if token := bearerToken(r.Header.Get("Authorization")); token != "" {
			r = r.WithContext(WithAuthToken(r.Context(), token))
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) string {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}
