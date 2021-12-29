package consensus

import (
	"crypto/sha256"
	"errors"

	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
)

// privateKey is a wrapper around keys.PrivateKey
// which implements crypto.PrivateKey interface.
type privateKey struct {
	*keys.PrivateKey
}

// MarshalBinary implements encoding.BinaryMarshaler interface.
func (p privateKey) MarshalBinary() (data []byte, err error) {
	return p.PrivateKey.Bytes(), nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler interface.
func (p *privateKey) UnmarshalBinary(data []byte) (err error) {
	p.PrivateKey, err = keys.NewPrivateKeyFromBytes(data)
	return
}

// Sign implements dbft's crypto.PrivateKey interface.
func (p *privateKey) Sign(data []byte) ([]byte, error) {
	return p.PrivateKey.Sign(data), nil
}

// publicKey is a wrapper around keys.PublicKey
// which implements crypto.PublicKey interface.
type publicKey struct {
	*keys.PublicKey
}

// MarshalBinary implements encoding.BinaryMarshaler interface.
func (p publicKey) MarshalBinary() (data []byte, err error) {
	return p.PublicKey.Bytes(), nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler interface.
func (p *publicKey) UnmarshalBinary(data []byte) error {
	return p.PublicKey.DecodeBytes(data)
}

// Verify implements crypto.PublicKey interface.
func (p publicKey) Verify(msg, sig []byte) error {
	hash := sha256.Sum256(msg)
	if p.PublicKey.Verify(sig, hash[:]) {
		return nil
	}

	return errors.New("error")
}
