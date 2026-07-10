/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package snell

import (
	"crypto/aes"
	"crypto/cipher"

	"golang.org/x/crypto/argon2"
)

// snellKDF is the Snell-specific KDF: Argon2id with t=3, m=8 KiB, p=1,
// 32-byte output. The first keySize bytes are used as the AEAD key.
func snellKDF(psk, salt []byte, keySize int) []byte {
	return argon2.IDKey(psk, salt, 3, 8, 1, 32)[:keySize]
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}
