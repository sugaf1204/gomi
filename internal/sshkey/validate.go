package sshkey

import (
	"errors"
	"strings"
)

var (
	ErrInvalidName      = errors.New("name is required")
	ErrInvalidPublicKey = errors.New("publicKey is required")
	ErrMalformedKey     = errors.New("publicKey: malformed SSH public key")
)

// knownKeyPrefixes lists the standard OpenSSH public key type prefixes.
var knownKeyPrefixes = []string{
	"ssh-rsa ",
	"ssh-ed25519 ",
	"ecdsa-sha2-nistp256 ",
	"ecdsa-sha2-nistp384 ",
	"ecdsa-sha2-nistp521 ",
	"ssh-dss ",
	"sk-ssh-ed25519@openssh.com ",
	"sk-ecdsa-sha2-nistp256@openssh.com ",
}

func ValidateSSHKey(k SSHKey) error {
	if strings.TrimSpace(k.Name) == "" {
		return ErrInvalidName
	}
	pubKey := strings.TrimSpace(k.PublicKey)
	if pubKey == "" {
		return ErrInvalidPublicKey
	}
	hasValidPrefix := false
	for _, prefix := range knownKeyPrefixes {
		if strings.HasPrefix(pubKey, prefix) {
			hasValidPrefix = true
			break
		}
	}
	if !hasValidPrefix {
		return ErrMalformedKey
	}
	return nil
}
