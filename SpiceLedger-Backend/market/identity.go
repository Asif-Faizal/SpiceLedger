package market

import (
	"context"
	"strings"

	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"google.golang.org/grpc/metadata"
)

// ResolveActorID prefers the authenticated account over a client-supplied user id.
// An admin may pass a user id to act for that merchant. A request with no
// authenticated account (internal basic auth, or a test) must name the user.
func ResolveActorID(ctx context.Context, requested string) (string, error) {
	accountID, _ := ctx.Value(util.AccountIDKey).(string)
	isAdmin, _ := ctx.Value(util.IsAdminKey).(bool)
	requested = strings.TrimSpace(requested)
	if accountID != "" && !isAdmin {
		return accountID, nil
	}
	if accountID != "" && isAdmin {
		if requested != "" {
			return requested, nil
		}
		return accountID, nil
	}
	if requested == "" {
		return "", invalidf("user_id is required")
	}
	return requested, nil
}

// IdempotencyKeyFromContext uses the explicit request field, then gRPC metadata.
// Metadata keys are lowercase. The client must send the same key on a retry.
func IdempotencyKeyFromContext(ctx context.Context, explicit string) string {
	if key := strings.TrimSpace(explicit); key != "" {
		return key
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("idempotency-key")
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}
