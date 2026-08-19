//go:build go1.27

package reality

import (
	"crypto/mldsa"
)

type mldsaPublicKey struct {
	*mldsa.PublicKey
}

type mldsaOptions struct {
	*mldsa.Options
}

type mldsaParameters struct {
	mldsa.Parameters
}

func mldsa65() mldsaParameters {
	return mldsaParameters{
		Parameters: mldsa.MLDSA65(),
	}
}

func mldsaVerify(pk *mldsaPublicKey, message []byte, signature []byte, opts *mldsaOptions) error {
	return mldsa.Verify(pk.PublicKey, message, signature, opts.Options)
}

func mldsaNewPublicKey(params mldsaParameters, encoding []byte) (*mldsaPublicKey, error) {
	pk, err := mldsa.NewPublicKey(params.Parameters, encoding)
	if err != nil {
		return nil, err
	}
	return &mldsaPublicKey{
		PublicKey: pk,
	}, nil
}
