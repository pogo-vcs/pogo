package db

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

func DecodeToken(token string) ([]byte, error) {
	// try to detect std or url encoding
	if strings.ContainsAny(token, "+/") {
		// must be std encoding
		if tokenBytes, err := base64.StdEncoding.DecodeString(token); err == nil {
			return tokenBytes, nil
		} else {
			return nil, fmt.Errorf("failed to decode %q: %w", token, err)
		}
	} else if strings.ContainsAny(token, "-_") {
		// must be url encoding
		if tokenBytes, err := base64.URLEncoding.DecodeString(token); err == nil {
			return tokenBytes, nil
		} else {
			return nil, fmt.Errorf("failed to decode %q: %w", token, err)
		}
	}

	// try base64 url
	if tokenBytes, err := base64.URLEncoding.DecodeString(token); err == nil {
		return tokenBytes, nil
	} else {
		// try base64 std
		if tokenBytes, err1 := base64.StdEncoding.DecodeString(token); err1 == nil {
			return tokenBytes, nil
		} else {
			return nil, fmt.Errorf("failed to decode %q: %w", token, errors.Join(err, err1))
		}
	}
}

func EncodeToken(token []byte) string {
	return base64.URLEncoding.EncodeToString(token)
}
