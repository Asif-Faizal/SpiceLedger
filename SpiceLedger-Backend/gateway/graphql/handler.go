package graphql

import (
	"context"
	"net/http"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc/status"
)

// NewHandler configures and returns the GraphQL HTTP handler with professional middleware
func NewHandler(s *Server) http.Handler {
	srv := handler.New(s.ToExecutableSchema())

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.MultipartForm{})

	srv.Use(extension.Introspection{})

	srv.SetErrorPresenter(func(ctx context.Context, err error) *gqlerror.Error {
		gqlErr := graphql.DefaultErrorPresenter(ctx, err)
		if st, ok := status.FromError(err); ok {
			gqlErr.Message = st.Message()
			if gqlErr.Extensions == nil {
				gqlErr.Extensions = make(map[string]interface{})
			}
			gqlErr.Extensions["grpc_code"] = st.Code().String()
		}
		return gqlErr
	})

	return restResponseEnvelopeMiddleware(authMiddleware(srv))
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if authHeader := r.Header.Get("Authorization"); authHeader != "" {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			ctx = context.WithValue(ctx, util.AccessTokenKey, tokenString)
		}
		// Trading retries send Idempotency-Key. The mutation argument wins when both are set.
		if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
			ctx = context.WithValue(ctx, util.IdempotencyKey, key)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
