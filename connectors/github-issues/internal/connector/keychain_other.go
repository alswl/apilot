//go:build !darwin || !cgo

package connector

import "errors"

func readCredential(string, string) ([]byte, error) {
	return nil, errors.New("GitHub Keychain is unavailable on this build")
}

func writeCredential(string, string, []byte) error {
	return errors.New("GitHub Keychain is unavailable on this build")
}
