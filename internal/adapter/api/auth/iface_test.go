package auth_test

import (
	"dconnector/internal/adapter/api"
	"dconnector/internal/adapter/api/auth"
)

// *auth.Service debe satisfacer api.Auth.
var _ api.Auth = (*auth.Service)(nil)
