package all

import (
	// The following are necessary as they register handlers in their init functions.

	// Mandatory features. Can't remove unless there are replacements.
	_ "github.com/exclavenetwork/exclave-core/v5/app/dispatcher"
	_ "github.com/exclavenetwork/exclave-core/v5/app/proxyman/inbound"
	_ "github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"

	// Default commander and all its services. This is an optional feature.
	_ "github.com/exclavenetwork/exclave-core/v5/app/commander"
	_ "github.com/exclavenetwork/exclave-core/v5/app/log/command"
	_ "github.com/exclavenetwork/exclave-core/v5/app/proxyman/command"
	_ "github.com/exclavenetwork/exclave-core/v5/app/router/command"
	_ "github.com/exclavenetwork/exclave-core/v5/app/stats/command"

	// Developer preview services
	_ "github.com/exclavenetwork/exclave-core/v5/app/observatory/command"

	// Other optional features.
	_ "github.com/exclavenetwork/exclave-core/v5/app/browserdialer"
	_ "github.com/exclavenetwork/exclave-core/v5/app/browserforwarder"
	_ "github.com/exclavenetwork/exclave-core/v5/app/dns"
	_ "github.com/exclavenetwork/exclave-core/v5/app/dns/fakedns"
	_ "github.com/exclavenetwork/exclave-core/v5/app/log"
	_ "github.com/exclavenetwork/exclave-core/v5/app/policy"
	_ "github.com/exclavenetwork/exclave-core/v5/app/router"
	_ "github.com/exclavenetwork/exclave-core/v5/app/stats"

	// Fix dependency cycle caused by core import in internet package
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tagged/taggedimpl"

	// Developer preview features
	_ "github.com/exclavenetwork/exclave-core/v5/app/observatory"
	_ "github.com/exclavenetwork/exclave-core/v5/app/observatory/burst"
	_ "github.com/exclavenetwork/exclave-core/v5/app/observatory/multiobservatory"
	_ "github.com/exclavenetwork/exclave-core/v5/app/persistentstorage/filesystemstorage"

	// Inbound and outbound proxies.
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/blackhole"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/dns"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/dokodemo"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/freedom"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/http"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/http3"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/loopback"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/mixed"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/shadowsocks"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/shadowsocks_2022"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/sip003"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/sip003/external"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/sip003/self"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/socks"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/ssh"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/trojan"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/vless/inbound"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/vless/outbound"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/vmess/inbound"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/vmess/outbound"

	// Developer preview proxies
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/anytls"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/hysteria2"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/juicity"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/mieru"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/snell"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/tuic"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/wireguard"

	_ "github.com/exclavenetwork/exclave-core/v5/proxy/ipc"

	// Transports
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/domainsocket"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/grpc"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/http"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/kcp"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/quic"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/reality"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tcp"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls/utls"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/udp"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/websocket"

	// Developer preview transports
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/assembly"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/assembler/simple"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/roundtripper/httprt"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/assembler/packetconn"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/stereotype/meek"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/request/stereotype/mekya"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/httpupgrade"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/hysteria2"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/splithttp"

	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tlsmirror/mirrorenrollment/roundtripperenrollmentconfirmation"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/tlsmirror/server"

	// Transport headers
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/http"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/noop"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/srtp"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/tls"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/utp"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/wechat"
	_ "github.com/exclavenetwork/exclave-core/v5/transport/internet/headers/wireguard"

	// Geo loaders
	_ "github.com/exclavenetwork/exclave-core/v5/infra/conf/geodata/memconservative"
	_ "github.com/exclavenetwork/exclave-core/v5/infra/conf/geodata/standard"

	// JSON config support. (jsonv4) This disable selective compile
	_ "github.com/exclavenetwork/exclave-core/v5/main/formats"

	// commands
	_ "github.com/exclavenetwork/exclave-core/v5/main/commands/all"

	// Commands that rely on jsonv4 format This disable selective compile
	_ "github.com/exclavenetwork/exclave-core/v5/main/commands/all/api/jsonv4"
	_ "github.com/exclavenetwork/exclave-core/v5/main/commands/all/jsonv4"

	// V5 version of json configure file parser
	_ "github.com/exclavenetwork/exclave-core/v5/infra/conf/v5cfg"

	// Simplified config
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/http/simplified"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/mixed/simplified"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/shadowsocks/simplified"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/socks/simplified"
	_ "github.com/exclavenetwork/exclave-core/v5/proxy/trojan/simplified"
)
