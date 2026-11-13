package meta

import (
	"errors"

	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

// AuthProblem clasifica un error de conexión de la librería de Meta.
type AuthProblem int

const (
	// ProblemTransient: error de red o de servidor; se reintenta.
	ProblemTransient AuthProblem = iota
	// ProblemLoggedOut: la sesión ya no vale; hay que volver a vincular.
	ProblemLoggedOut
	// ProblemVerify: Meta pide verificar la cuenta en su sitio web (checkpoint, desafío, consentimiento).
	ProblemVerify
	// ProblemSuspended: cuenta suspendida.
	ProblemSuspended
)

// ClassifyAuthError traduce un error de la librería (messagix / instameow) y da el texto para el usuario.
// name es «Messenger» o «Instagram»; site, el sitio donde verificar («messenger.com» / «instagram.com»).
func ClassifyAuthError(name, site string, err error) (AuthProblem, string) {
	if err == nil {
		return ProblemTransient, ""
	}
	var ls *types.ErrorResponse
	switch {
	case errors.Is(err, httpclient.ErrAccountSuspended):
		return ProblemSuspended, "Meta suspendió esta cuenta de " + name + ". Revisa " + site + " en el navegador."
	case errors.Is(err, httpclient.ErrChallengeRequired), errors.Is(err, httpclient.ErrCheckpointRequired),
		errors.Is(err, httpclient.ErrConsentRequired):
		return ProblemVerify, "Meta pide verificar tu cuenta de " + name + ". Hazlo en " + site + " en el navegador y vuelve a pegar las cookies."
	case errors.Is(err, httpclient.ErrTokenInvalidated), errors.Is(err, httpclient.ErrUserIDIsZero):
		return ProblemLoggedOut, "La sesión de " + name + " ya no es válida. Vuelve a vincular la cuenta."
	case errors.As(err, &ls) && ls.ErrorCode == 1357053:
		return ProblemLoggedOut, "La sesión de " + name + " ya no es válida. Vuelve a vincular la cuenta."
	}
	return ProblemTransient, "No se pudo conectar con " + name + ". Revisa tu conexión a internet."
}
