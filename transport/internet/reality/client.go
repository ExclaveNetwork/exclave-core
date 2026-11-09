package reality

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/mldsa"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/exclavenetwork/reality"
	"golang.org/x/net/http2"

	"github.com/exclavenetwork/exclave-core/v5/common/dice"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

func Client(ctx context.Context, conn net.Conn, dest net.Destination, config *Config, opts ...option) (net.Conn, error) {
	if len(config.Fingerprint) > 0 {
		return uclient(ctx, conn, dest, config, opts...)
	}
	return client(ctx, conn, dest, config, opts...)
}

func client(ctx context.Context, conn net.Conn, dest net.Destination, config *Config, opts ...option) (net.Conn, error) {
	serverName := config.ServerName
	if len(serverName) == 0 {
		if dest.Address.Family().IsDomain() {
			serverName = dest.Address.Domain()
		} else {
			serverName = dest.Address.IP().String()
		}
	}
	var mldsaPublicKey *mldsa.PublicKey
	if len(config.Mldsa65Verify) > 0 {
		publicKey, err := mldsa.NewPublicKey(mldsa.MLDSA65(), config.Mldsa65Verify)
		if err != nil {
			return nil, err
		}
		mldsaPublicKey = publicKey
	}
	verified := false
	realityConfig := &reality.Config{
		ServerName:             serverName,
		SessionTicketsDisabled: true,
		InsecureSkipVerify:     true,
		RealityClientConfig: reality.RealityClientConfig{
			PublicKey:     config.PublicKey,
			ShortId:       [8]byte(config.ShortId),
			ClientVersion: [3]byte{25, 5, 16},
		},
		VerifyConnection: func(state reality.ConnectionState) error {
			if publicKey, ok := state.PeerCertificates[0].PublicKey.(ed25519.PublicKey); ok {
				authKey, _ := state.RealityAuthKey()
				h := hmac.New(sha512.New, authKey)
				h.Write(publicKey)
				if bytes.Equal(h.Sum(nil), state.PeerCertificates[0].Signature) {
					if mldsaPublicKey != nil {
						if len(state.PeerCertificates[0].Extensions) > 0 {
							clientHello, _ := state.RawClientHello()
							serverHello, _ := state.RawServerHello()
							h.Write(clientHello)
							h.Write(serverHello)
							if err := mldsa.Verify(mldsaPublicKey, h.Sum(nil), state.PeerCertificates[0].Extensions[0].Value, nil); err == nil {
								verified = true
								return nil
							}
						}
					} else {
						verified = true
						return nil
					}
				}
			}
			opts := x509.VerifyOptions{
				DNSName:       serverName,
				Intermediates: x509.NewCertPool(),
			}
			for _, cert := range state.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			if _, err := state.PeerCertificates[0].Verify(opts); err != nil {
				return err
			}
			return nil
		},
	}
	if config.DisableX25519Mlkem768 {
		realityConfig.CurvePreferences = []reality.CurveID{reality.X25519, reality.CurveP256, reality.CurveP384, reality.CurveP521}
	}
	for _, opt := range opts {
		opt(realityConfig)
	}
	realityConn := reality.Client(conn, realityConfig)
	if err := realityConn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	if !verified {
		go func() {
			client := &http.Client{
				Transport: &http2.Transport{
					DialTLSContext: func(_ context.Context, _, _ string, _ *tls.Config) (net.Conn, error) {
						return realityConn, nil
					},
				},
			}
			u := &url.URL{
				Scheme: "https",
				Host:   serverName,
			}
			req, err := http.NewRequest(http.MethodGet, u.String(), nil)
			if err != nil {
				return
			}
			req.AddCookie(&http.Cookie{
				Name:  "padding",
				Value: strings.Repeat("0", dice.Roll(32)+30),
			})
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
		return nil, newError("REALITY: processed invalid connection")
	}
	return &Conn{Conn: realityConn}, nil
}
