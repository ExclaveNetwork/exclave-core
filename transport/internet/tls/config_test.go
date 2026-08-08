package tls_test

import (
	gotls "crypto/tls"
	"crypto/x509"
	"reflect"
	"testing"
	"time"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/protocol/tls/cert"
	. "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls"
)

func TestCertificateIssuing(t *testing.T) {
	certificate := ParseCertificate(cert.MustGenerate(nil, cert.Authority(true), cert.KeyUsage(x509.KeyUsageCertSign)))
	certificate.Usage = Certificate_AUTHORITY_ISSUE

	c := &Config{
		Certificate: []*Certificate{
			certificate,
		},
	}

	tlsConfig := c.GetTLSConfig()
	v2rayCert, err := tlsConfig.GetCertificate(&gotls.ClientHelloInfo{
		ServerName: "www.v2fly.org",
	})
	common.Must(err)

	x509Cert, err := x509.ParseCertificate(v2rayCert.Certificate[0])
	common.Must(err)
	if !x509Cert.NotAfter.After(time.Now()) {
		t.Error("NotAfter: ", x509Cert.NotAfter)
	}
}

func TestExpiredCertificate(t *testing.T) {
	caCert := cert.MustGenerate(nil, cert.Authority(true), cert.KeyUsage(x509.KeyUsageCertSign))
	expiredCert := cert.MustGenerate(caCert, cert.NotAfter(time.Now().Add(time.Minute*-2)), cert.CommonName("www.v2fly.org"), cert.DNSNames("www.v2fly.org"))

	certificate := ParseCertificate(caCert)
	certificate.Usage = Certificate_AUTHORITY_ISSUE

	certificate2 := ParseCertificate(expiredCert)

	c := &Config{
		Certificate: []*Certificate{
			certificate,
			certificate2,
		},
	}

	tlsConfig := c.GetTLSConfig()
	v2rayCert, err := tlsConfig.GetCertificate(&gotls.ClientHelloInfo{
		ServerName: "www.v2fly.org",
	})
	common.Must(err)

	x509Cert, err := x509.ParseCertificate(v2rayCert.Certificate[0])
	common.Must(err)
	if !x509Cert.NotAfter.After(time.Now()) {
		t.Error("NotAfter: ", x509Cert.NotAfter)
	}
}

func TestInsecureCertificates(t *testing.T) {
	c := &Config{}

	tlsConfig := c.GetTLSConfig()
	if len(tlsConfig.CipherSuites) > 0 {
		t.Fatal("Unexpected tls cipher suites list: ", tlsConfig.CipherSuites)
	}
}

func BenchmarkCertificateIssuing(b *testing.B) {
	certificate := ParseCertificate(cert.MustGenerate(nil, cert.Authority(true), cert.KeyUsage(x509.KeyUsageCertSign)))
	certificate.Usage = Certificate_AUTHORITY_ISSUE

	c := &Config{
		Certificate: []*Certificate{
			certificate,
		},
	}

	tlsConfig := c.GetTLSConfig()
	lenCerts := len(tlsConfig.Certificates)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = tlsConfig.GetCertificate(&gotls.ClientHelloInfo{
			ServerName: "www.v2fly.org",
		})
		delete(tlsConfig.NameToCertificate, "www.v2fly.org")
		tlsConfig.Certificates = tlsConfig.Certificates[:lenCerts]
	}
}

func TestClone(t *testing.T) {
	config1 := &Config{
		AllowInsecure: true,
		Certificate: []*Certificate{
			{
				Certificate: []byte{1},
				Key:         []byte{1},
			},
		},
		ServerName:                           "1",
		NextProtocol:                         []string{"1"},
		DisableSystemRoot:                    true,
		PinnedPeerCertificateChainSha256:     [][]byte{{1}},
		VerifyClientCertificate:              true,
		MinVersion:                           Config_TLS1_0,
		MaxVersion:                           Config_TLS1_0,
		AllowInsecureIfPinnedPeerCertificate: true,
		Ciphersuites:                         []uint32{1},
		PinnedPeerCertificatePublicKeySha256: [][]byte{{1}},
		PinnedPeerCertificateSha256:          []string{"1"},
		ServerNameToVerify:                   []string{"1"},
		Ech: &Config_ECH{
			Enabled:     true,
			Config:      []byte{1},
			QueryDomain: "1",
			Key:         []byte{1},
		},
	}
	for i := 0; i < reflect.ValueOf(config1).Elem().Type().NumField(); i++ {
		field := reflect.ValueOf(config1).Elem().Type().Field(i)
		switch name := field.Name; name {
		case "AllowInsecure", "Certificate", "ServerName", "NextProtocol", "DisableSystemRoot",
			"PinnedPeerCertificateChainSha256", "VerifyClientCertificate", "MinVersion",
			"MaxVersion", "AllowInsecureIfPinnedPeerCertificate", "Ciphersuites",
			"PinnedPeerCertificatePublicKeySha256", "PinnedPeerCertificateSha256",
			"ServerNameToVerify", "Ech":
		default:
			if !field.IsExported() {
				continue
			}
			t.Errorf("unknown field %q", name)
		}
	}
	config2 := config1.Clone()
	if !reflect.DeepEqual(config1, config2) {
		t.Errorf("failed to copy a field")
	}
	for i := 0; i < reflect.ValueOf(config1.Ech).Elem().Type().NumField(); i++ {
		field := reflect.ValueOf(config1.Ech).Elem().Type().Field(i)
		switch name := field.Name; name {
		case "Enabled", "Config", "QueryDomain", "Key":
		default:
			if !field.IsExported() {
				continue
			}
			t.Errorf("unknown field %q", name)
		}
	}
	if !reflect.DeepEqual(config1.Ech, config2.Ech) {
		t.Errorf("failed to copy a field")
	}
}
