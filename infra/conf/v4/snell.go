package v4

import (
	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell"
)

// SnellClientConfig is the JSON configuration for Snell outbound.
//
// Validation is strict:
//   - version: 4 or 6 only (0 defaults to 4)
//   - obfsMode: exact "none" | "http" | "tls" (empty defaults to "none"; "off" is not accepted)
//   - mode (v6 only): exact "default" | "unshaped" | "unsafe-raw" (empty defaults to "default")
//   - v4-only fields must be empty on v6, and v6-only fields must be empty on v4
//   - empty PSK is allowed
//   - userPSK is a sing-box private extension (optional)
type SnellClientConfig struct {
	Address *cfgcommon.Address `json:"address"`
	Port    uint16             `json:"port"`
	PSK     string             `json:"psk"`
	// UserPSK is only compatible with sing-box Snell server.
	UserPSK  string `json:"userPSK"`
	ObfsMode string `json:"obfsMode"`
	// ObfsHost is v4-only.
	ObfsHost string `json:"obfsHost"`
	Version  uint32 `json:"version"`
	Reuse    bool   `json:"reuse"`
	// Mode is v6-only.
	Mode string `json:"mode"`
}

func (c *SnellClientConfig) Build() (proto.Message, error) {
	if c.Address == nil {
		return nil, newError("Snell: missing server address")
	}

	version := c.Version
	if version == 0 {
		version = 4
	}
	if version != 4 && version != 6 {
		return nil, newError("Snell: version must be 4 or 6, got ", version)
	}

	obfsMode := c.ObfsMode
	if obfsMode == "" {
		obfsMode = "none"
	}
	switch obfsMode {
	case "none", "http", "tls":
	default:
		return nil, newError(`Snell: obfsMode must be "none", "http" or "tls", got `, c.ObfsMode)
	}

	mode := c.Mode
	switch version {
	case 6:
		if mode == "" {
			mode = "default"
		}
		switch mode {
		case "default", "unshaped", "unsafe-raw":
		default:
			return nil, newError(`Snell: mode must be "default", "unshaped" or "unsafe-raw", got `, c.Mode)
		}
		// v4-only fields: obfsHost, and non-default obfuscation
		if c.ObfsHost != "" {
			return nil, newError("Snell: obfsHost is only valid for version 4")
		}
		if c.ObfsMode != "" && c.ObfsMode != "none" {
			return nil, newError("Snell: obfsMode is only valid for version 4 (v6 has no obfuscation)")
		}
		obfsMode = "none"
	case 4:
		if c.Mode != "" {
			return nil, newError("Snell: mode is only valid for version 6")
		}
		mode = ""
	}

	return &snell.ClientConfig{
		Address:  c.Address.Build(),
		Port:     uint32(c.Port),
		Psk:      c.PSK,
		UserPsk:  c.UserPSK,
		ObfsMode: obfsMode,
		ObfsHost: c.ObfsHost,
		Version:  version,
		Reuse:    c.Reuse,
		Mode:     mode,
	}, nil
}
