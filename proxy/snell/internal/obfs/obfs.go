/*
 * Based on opensnell (GPL-3.0-or-later).
 * Client-side obfuscation wrapper for Snell.
 */
package obfs

import (
	"fmt"
	"net"

	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/obfs/http"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/obfs/tls"
)

// NewClient wraps conn with the requested obfuscation mode.
// mode: "off"/"none"/"" | "http" | "tls"
func NewClient(conn net.Conn, server, port, mode string) (net.Conn, error) {
	switch mode {
	case "tls":
		return tls.NewTLSObfsClient(conn, server), nil
	case "http":
		return http.NewHTTPObfsClient(conn, server, port), nil
	case "none", "off", "":
		return conn, nil
	default:
		return nil, fmt.Errorf("invalid snell obfs type %q", mode)
	}
}
