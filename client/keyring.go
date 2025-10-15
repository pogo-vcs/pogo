//go:build !fakekeyring

package client

import (
	"github.com/zalando/go-keyring"
)

func keyringGet(service string, user string) (string, error) {
	return keyring.Get(service, user)
}

func keyringSet(service string, user string, password string) error {
	return keyring.Set(service, user, password)
}

func keyringDelete(service string, user string) error {
	return keyring.Delete(service, user)
}