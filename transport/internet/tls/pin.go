package tls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
)

func CalculatePEMCertChainSHA256Hash(certContent []byte) (string, error) {
	var hashValue []byte
	ok := false
	for {
		block, remain := pem.Decode(certContent)
		if block == nil {
			break
		}
		hash := sha256.Sum256(block.Bytes)
		if hashValue == nil {
			hashValue = hash[:]
		} else {
			newHashValue := sha256.Sum256(append(hashValue, hash[:]...))
			hashValue = newHashValue[:]
		}
		certContent = remain
		ok = true
	}
	if !ok {
		return "", newError("invalid certificate")
	}
	return base64.StdEncoding.EncodeToString(hashValue), nil
}

func CalculatePEMCertPublicKeySHA256Hash(certContent []byte) (string, error) {
	block, _ := pem.Decode(certContent)
	if block == nil {
		return "", newError("invalid certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(hash[:]), nil
}

func CalculatePEMCertSHA256Hash(certContent []byte) (string, error) {
	block, _ := pem.Decode(certContent)
	if block == nil {
		return "", newError("invalid certificate")
	}
	hash := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(hash[:]), nil
}
