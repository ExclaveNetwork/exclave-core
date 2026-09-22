package tcp

import (
	"context"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/serial"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/reality"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/security"
)

// Dial dials a new TCP connection to the given destination.
func Dial(ctx context.Context, dest net.Destination, streamSettings *internet.MemoryStreamConfig) (internet.Connection, error) {
	newError("dialing TCP to ", dest).WriteToLog(session.ExportIDToError(ctx))
	conn, err := internet.DialSystem(ctx, dest, streamSettings.SocketSettings)
	if err != nil {
		return nil, err
	}

	securityEngine, _ := security.CreateSecurityEngineFromSettings(ctx, streamSettings)

	if securityEngine != nil {
		securityConn, err := securityEngine.Client(conn, security.OptionWithDestination{Dest: dest})
		if err != nil {
			conn.Close()
			return nil, newError("unable to create security protocol client from security engine").Base(err)
		}
		conn = securityConn
	} else if config := reality.ConfigFromStreamSettings(streamSettings); config != nil {
		realityConn, err := reality.Client(ctx, conn, dest, config)
		if err != nil {
			conn.Close()
			return nil, err
		}
		conn = realityConn
	}

	tcpSettings := streamSettings.ProtocolSettings.(*Config)
	if tcpSettings.HeaderSettings != nil {
		headerConfig, err := serial.GetInstanceOf(tcpSettings.HeaderSettings)
		if err != nil {
			conn.Close()
			return nil, newError("failed to get header settings").Base(err).AtError()
		}
		auth, err := internet.CreateConnectionAuthenticator(headerConfig)
		if err != nil {
			conn.Close()
			return nil, newError("failed to create header authenticator").Base(err).AtError()
		}
		conn = auth.Client(conn)
	}
	return internet.Connection(conn), nil
}

func init() {
	common.Must(internet.RegisterTransportDialer(protocolName, Dial))
}
