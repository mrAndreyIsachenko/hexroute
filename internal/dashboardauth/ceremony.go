package dashboardauth

import (
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type Ceremony interface {
	BeginRegistration(User) (
		*protocol.CredentialCreation,
		*webauthn.SessionData,
		error,
	)
	FinishRegistration(
		User,
		webauthn.SessionData,
		*http.Request,
	) (*webauthn.Credential, error)
	BeginLogin(User) (
		*protocol.CredentialAssertion,
		*webauthn.SessionData,
		error,
	)
	FinishLogin(
		User,
		webauthn.SessionData,
		*http.Request,
	) (*webauthn.Credential, error)
}

type WebAuthnCeremony struct {
	relyingParty *webauthn.WebAuthn
}

func NewWebAuthnCeremony(
	relyingPartyID string,
	origin string,
) (*WebAuthnCeremony, error) {
	if relyingPartyID == "" || !validOrigin(origin) {
		return nil, ErrInvalidAuthConfig
	}
	relyingParty, err := webauthn.New(&webauthn.Config{
		RPID:          relyingPartyID,
		RPDisplayName: "Hexroute",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, ErrInvalidAuthConfig
	}
	return &WebAuthnCeremony{relyingParty: relyingParty}, nil
}

func (ceremony *WebAuthnCeremony) BeginRegistration(
	user User,
) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	return ceremony.relyingParty.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(
			protocol.ResidentKeyRequirementRequired,
		),
		webauthn.WithAuthenticatorSelection(
			protocol.AuthenticatorSelection{
				ResidentKey:      protocol.ResidentKeyRequirementRequired,
				UserVerification: protocol.VerificationRequired,
			},
		),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
}

func (ceremony *WebAuthnCeremony) FinishRegistration(
	user User,
	session webauthn.SessionData,
	request *http.Request,
) (*webauthn.Credential, error) {
	return ceremony.relyingParty.FinishRegistration(user, session, request)
}

func (ceremony *WebAuthnCeremony) BeginLogin(
	user User,
) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return ceremony.relyingParty.BeginLogin(
		user,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
}

func (ceremony *WebAuthnCeremony) FinishLogin(
	user User,
	session webauthn.SessionData,
	request *http.Request,
) (*webauthn.Credential, error) {
	return ceremony.relyingParty.FinishLogin(user, session, request)
}
