package sshkey

import (
	"time"
)

type SSHKey struct {
	Name string `json:"name"`

	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey,omitempty"`
	Comment    string `json:"comment,omitempty"`
	KeyType    string `json:"keyType,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
