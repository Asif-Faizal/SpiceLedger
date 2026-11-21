package util

type ContextKey string

const (
	UserTypeSuperAdmin = "super_admin"
	UserTypeAdmin      = "admin"
	UserTypeMerchant   = "merchant"
	UserTypeCustomer   = "customer"

	AccountIDKey       ContextKey = "account_id"
	UserTypeKey        ContextKey = "user_type"
	EmailKey           ContextKey = "email"
	IsAdminKey         ContextKey = "is_admin"
	IsMerchantKey      ContextKey = "is_merchant"
	IsAuthenticatedKey ContextKey = "is_authenticated"
	AccessTokenKey     ContextKey = "access_token"
	// IdempotencyKey is the client retry token copied from the Idempotency-Key header.
	IdempotencyKey ContextKey = "idempotency_key"
)
