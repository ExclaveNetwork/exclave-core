//go:build !go1.27

package reality

import (
	"filippo.io/mldsa"
)

type mldsaVerify struct {
	publicKey *mldsa.PublicKey
}

func (v *mldsaVerify) verify(message, signature []byte) error {
	return mldsa.Verify(v.publicKey, message, signature, nil)
}

func newMLDSA65Verify(encoding []byte) (*mldsaVerify, error) {
	publicKey, err := mldsa.NewPublicKey(mldsa.MLDSA65(), encoding)
	if err != nil {
		return nil, err
	}
	return &mldsaVerify{
		publicKey: publicKey,
	}, nil
}
