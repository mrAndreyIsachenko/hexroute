//go:build !darwin || !cgo

package policyapproval

import "context"

type UserPresenceKeychainStore struct{}

func (UserPresenceKeychainStore) StoreUserPresence(
	context.Context,
	string,
	string,
	[]byte,
) error {
	return ErrKeychainAccess
}

func (UserPresenceKeychainStore) ReadUserPresence(
	context.Context,
	string,
	string,
) ([]byte, error) {
	return nil, ErrKeychainAccess
}

var _ KeychainProvisioner = UserPresenceKeychainStore{}
var _ KeychainReader = UserPresenceKeychainStore{}
