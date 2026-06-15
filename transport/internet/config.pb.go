package internet

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	anypb "google.golang.org/protobuf/types/known/anypb"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// MPTCP is the state of MPTCP settings.
// Define it here to avoid conflict with TCPFastOpenState.
type MPTCPState int32

const (
	// AsIs is to leave the current MPTCP state as is, unmodified.
	MPTCPState_AsIs MPTCPState = 0
	// Enable is for enabling MPTCP explictly.
	MPTCPState_Enable MPTCPState = 1
	// Disable is for disabling MPTCP explictly.
	MPTCPState_Disable MPTCPState = 2
)

// Enum value maps for MPTCPState.
var (
	MPTCPState_name = map[int32]string{
		0: "AsIs",
		1: "Enable",
		2: "Disable",
	}
	MPTCPState_value = map[string]int32{
		"AsIs":    0,
		"Enable":  1,
		"Disable": 2,
	}
)

func (x MPTCPState) Enum() *MPTCPState {
	p := new(MPTCPState)
	*p = x
	return p
}

func (x MPTCPState) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (MPTCPState) Descriptor() protoreflect.EnumDescriptor {
	return file_transport_internet_config_proto_enumTypes[0].Descriptor()
}

func (MPTCPState) Type() protoreflect.EnumType {
	return &file_transport_internet_config_proto_enumTypes[0]
}

func (x MPTCPState) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use MPTCPState.Descriptor instead.
func (MPTCPState) EnumDescriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{0}
}

type SocketConfig_TCPFastOpenState int32

const (
	// AsIs is to leave the current TFO state as is, unmodified.
	SocketConfig_AsIs SocketConfig_TCPFastOpenState = 0
	// Enable is for enabling TFO explictly.
	SocketConfig_Enable SocketConfig_TCPFastOpenState = 1
	// Disable is for disabling TFO explictly.
	SocketConfig_Disable SocketConfig_TCPFastOpenState = 2
)

// Enum value maps for SocketConfig_TCPFastOpenState.
var (
	SocketConfig_TCPFastOpenState_name = map[int32]string{
		0: "AsIs",
		1: "Enable",
		2: "Disable",
	}
	SocketConfig_TCPFastOpenState_value = map[string]int32{
		"AsIs":    0,
		"Enable":  1,
		"Disable": 2,
	}
)

func (x SocketConfig_TCPFastOpenState) Enum() *SocketConfig_TCPFastOpenState {
	p := new(SocketConfig_TCPFastOpenState)
	*p = x
	return p
}

func (x SocketConfig_TCPFastOpenState) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SocketConfig_TCPFastOpenState) Descriptor() protoreflect.EnumDescriptor {
	return file_transport_internet_config_proto_enumTypes[1].Descriptor()
}

func (SocketConfig_TCPFastOpenState) Type() protoreflect.EnumType {
	return &file_transport_internet_config_proto_enumTypes[1]
}

func (x SocketConfig_TCPFastOpenState) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SocketConfig_TCPFastOpenState.Descriptor instead.
func (SocketConfig_TCPFastOpenState) EnumDescriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{3, 0}
}

type SocketConfig_TProxyMode int32

const (
	// TProxy is off.
	SocketConfig_Off SocketConfig_TProxyMode = 0
	// TProxy mode.
	SocketConfig_TProxy SocketConfig_TProxyMode = 1
	// Redirect mode.
	SocketConfig_Redirect SocketConfig_TProxyMode = 2
)

// Enum value maps for SocketConfig_TProxyMode.
var (
	SocketConfig_TProxyMode_name = map[int32]string{
		0: "Off",
		1: "TProxy",
		2: "Redirect",
	}
	SocketConfig_TProxyMode_value = map[string]int32{
		"Off":      0,
		"TProxy":   1,
		"Redirect": 2,
	}
)

func (x SocketConfig_TProxyMode) Enum() *SocketConfig_TProxyMode {
	p := new(SocketConfig_TProxyMode)
	*p = x
	return p
}

func (x SocketConfig_TProxyMode) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SocketConfig_TProxyMode) Descriptor() protoreflect.EnumDescriptor {
	return file_transport_internet_config_proto_enumTypes[2].Descriptor()
}

func (SocketConfig_TProxyMode) Type() protoreflect.EnumType {
	return &file_transport_internet_config_proto_enumTypes[2]
}

func (x SocketConfig_TProxyMode) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SocketConfig_TProxyMode.Descriptor instead.
func (SocketConfig_TProxyMode) EnumDescriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{3, 1}
}

type TransportConfig struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Type of network that this settings supports.
	ProtocolName string `protobuf:"bytes,3,opt,name=protocol_name,json=protocolName,proto3" json:"protocol_name,omitempty"`
	// Specific settings. Must be of the transports.
	Settings      *anypb.Any `protobuf:"bytes,2,opt,name=settings,proto3" json:"settings,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *TransportConfig) Reset() {
	*x = TransportConfig{}
	mi := &file_transport_internet_config_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *TransportConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TransportConfig) ProtoMessage() {}

func (x *TransportConfig) ProtoReflect() protoreflect.Message {
	mi := &file_transport_internet_config_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TransportConfig.ProtoReflect.Descriptor instead.
func (*TransportConfig) Descriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{0}
}

func (x *TransportConfig) GetProtocolName() string {
	if x != nil {
		return x.ProtocolName
	}
	return ""
}

func (x *TransportConfig) GetSettings() *anypb.Any {
	if x != nil {
		return x.Settings
	}
	return nil
}

type StreamConfig struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Effective network.
	ProtocolName      string             `protobuf:"bytes,5,opt,name=protocol_name,json=protocolName,proto3" json:"protocol_name,omitempty"`
	TransportSettings []*TransportConfig `protobuf:"bytes,2,rep,name=transport_settings,json=transportSettings,proto3" json:"transport_settings,omitempty"`
	// Type of security. Must be a message name of the settings proto.
	SecurityType string `protobuf:"bytes,3,opt,name=security_type,json=securityType,proto3" json:"security_type,omitempty"`
	// Settings for transport security. For now the only choice is TLS.
	SecuritySettings []*anypb.Any  `protobuf:"bytes,4,rep,name=security_settings,json=securitySettings,proto3" json:"security_settings,omitempty"`
	SocketSettings   *SocketConfig `protobuf:"bytes,6,opt,name=socket_settings,json=socketSettings,proto3" json:"socket_settings,omitempty"`
	unknownFields    protoimpl.UnknownFields
	sizeCache        protoimpl.SizeCache
}

func (x *StreamConfig) Reset() {
	*x = StreamConfig{}
	mi := &file_transport_internet_config_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StreamConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StreamConfig) ProtoMessage() {}

func (x *StreamConfig) ProtoReflect() protoreflect.Message {
	mi := &file_transport_internet_config_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StreamConfig.ProtoReflect.Descriptor instead.
func (*StreamConfig) Descriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{1}
}

func (x *StreamConfig) GetProtocolName() string {
	if x != nil {
		return x.ProtocolName
	}
	return ""
}

func (x *StreamConfig) GetTransportSettings() []*TransportConfig {
	if x != nil {
		return x.TransportSettings
	}
	return nil
}

func (x *StreamConfig) GetSecurityType() string {
	if x != nil {
		return x.SecurityType
	}
	return ""
}

func (x *StreamConfig) GetSecuritySettings() []*anypb.Any {
	if x != nil {
		return x.SecuritySettings
	}
	return nil
}

func (x *StreamConfig) GetSocketSettings() *SocketConfig {
	if x != nil {
		return x.SocketSettings
	}
	return nil
}

type ProxyConfig struct {
	state               protoimpl.MessageState `protogen:"open.v1"`
	Tag                 string                 `protobuf:"bytes,1,opt,name=tag,proto3" json:"tag,omitempty"`
	TransportLayerProxy bool                   `protobuf:"varint,2,opt,name=transportLayerProxy,proto3" json:"transportLayerProxy,omitempty"`
	unknownFields       protoimpl.UnknownFields
	sizeCache           protoimpl.SizeCache
}

func (x *ProxyConfig) Reset() {
	*x = ProxyConfig{}
	mi := &file_transport_internet_config_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ProxyConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ProxyConfig) ProtoMessage() {}

func (x *ProxyConfig) ProtoReflect() protoreflect.Message {
	mi := &file_transport_internet_config_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ProxyConfig.ProtoReflect.Descriptor instead.
func (*ProxyConfig) Descriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{2}
}

func (x *ProxyConfig) GetTag() string {
	if x != nil {
		return x.Tag
	}
	return ""
}

func (x *ProxyConfig) GetTransportLayerProxy() bool {
	if x != nil {
		return x.TransportLayerProxy
	}
	return false
}

// SocketConfig is options to be applied on network sockets.
type SocketConfig struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// Mark of the connection. If non-zero, the value will be set to SO_MARK.
	Mark uint32 `protobuf:"varint,1,opt,name=mark,proto3" json:"mark,omitempty"`
	// TFO is the state of TFO settings.
	Tfo SocketConfig_TCPFastOpenState `protobuf:"varint,2,opt,name=tfo,proto3,enum=exclave.core.transport.internet.SocketConfig_TCPFastOpenState" json:"tfo,omitempty"`
	// TProxy is for enabling TProxy socket option.
	Tproxy SocketConfig_TProxyMode `protobuf:"varint,3,opt,name=tproxy,proto3,enum=exclave.core.transport.internet.SocketConfig_TProxyMode" json:"tproxy,omitempty"`
	// ReceiveOriginalDestAddress is for enabling IP_RECVORIGDSTADDR socket
	// option. This option is for UDP only.
	ReceiveOriginalDestAddress bool              `protobuf:"varint,4,opt,name=receive_original_dest_address,json=receiveOriginalDestAddress,proto3" json:"receive_original_dest_address,omitempty"`
	AcceptProxyProtocol        bool              `protobuf:"varint,7,opt,name=accept_proxy_protocol,json=acceptProxyProtocol,proto3" json:"accept_proxy_protocol,omitempty"`
	TcpKeepAliveInterval       int32             `protobuf:"varint,8,opt,name=tcp_keep_alive_interval,json=tcpKeepAliveInterval,proto3" json:"tcp_keep_alive_interval,omitempty"`
	TfoQueueLength             uint32            `protobuf:"varint,9,opt,name=tfo_queue_length,json=tfoQueueLength,proto3" json:"tfo_queue_length,omitempty"`
	TcpKeepAliveIdle           int32             `protobuf:"varint,10,opt,name=tcp_keep_alive_idle,json=tcpKeepAliveIdle,proto3" json:"tcp_keep_alive_idle,omitempty"`
	BindToDevice               string            `protobuf:"bytes,11,opt,name=bind_to_device,json=bindToDevice,proto3" json:"bind_to_device,omitempty"`
	RxBufSize                  int64             `protobuf:"varint,12,opt,name=rx_buf_size,json=rxBufSize,proto3" json:"rx_buf_size,omitempty"`
	TxBufSize                  int64             `protobuf:"varint,13,opt,name=tx_buf_size,json=txBufSize,proto3" json:"tx_buf_size,omitempty"`
	ForceBufSize               bool              `protobuf:"varint,14,opt,name=force_buf_size,json=forceBufSize,proto3" json:"force_buf_size,omitempty"`
	Mptcp                      MPTCPState        `protobuf:"varint,15,opt,name=mptcp,proto3,enum=exclave.core.transport.internet.MPTCPState" json:"mptcp,omitempty"`
	TlsFragmentation           *TLSFragmentation `protobuf:"bytes,99,opt,name=tlsFragmentation,proto3" json:"tlsFragmentation,omitempty"`
	unknownFields              protoimpl.UnknownFields
	sizeCache                  protoimpl.SizeCache
}

func (x *SocketConfig) Reset() {
	*x = SocketConfig{}
	mi := &file_transport_internet_config_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SocketConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SocketConfig) ProtoMessage() {}

func (x *SocketConfig) ProtoReflect() protoreflect.Message {
	mi := &file_transport_internet_config_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SocketConfig.ProtoReflect.Descriptor instead.
func (*SocketConfig) Descriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{3}
}

func (x *SocketConfig) GetMark() uint32 {
	if x != nil {
		return x.Mark
	}
	return 0
}

func (x *SocketConfig) GetTfo() SocketConfig_TCPFastOpenState {
	if x != nil {
		return x.Tfo
	}
	return SocketConfig_AsIs
}

func (x *SocketConfig) GetTproxy() SocketConfig_TProxyMode {
	if x != nil {
		return x.Tproxy
	}
	return SocketConfig_Off
}

func (x *SocketConfig) GetReceiveOriginalDestAddress() bool {
	if x != nil {
		return x.ReceiveOriginalDestAddress
	}
	return false
}

func (x *SocketConfig) GetAcceptProxyProtocol() bool {
	if x != nil {
		return x.AcceptProxyProtocol
	}
	return false
}

func (x *SocketConfig) GetTcpKeepAliveInterval() int32 {
	if x != nil {
		return x.TcpKeepAliveInterval
	}
	return 0
}

func (x *SocketConfig) GetTfoQueueLength() uint32 {
	if x != nil {
		return x.TfoQueueLength
	}
	return 0
}

func (x *SocketConfig) GetTcpKeepAliveIdle() int32 {
	if x != nil {
		return x.TcpKeepAliveIdle
	}
	return 0
}

func (x *SocketConfig) GetBindToDevice() string {
	if x != nil {
		return x.BindToDevice
	}
	return ""
}

func (x *SocketConfig) GetRxBufSize() int64 {
	if x != nil {
		return x.RxBufSize
	}
	return 0
}

func (x *SocketConfig) GetTxBufSize() int64 {
	if x != nil {
		return x.TxBufSize
	}
	return 0
}

func (x *SocketConfig) GetForceBufSize() bool {
	if x != nil {
		return x.ForceBufSize
	}
	return false
}

func (x *SocketConfig) GetMptcp() MPTCPState {
	if x != nil {
		return x.Mptcp
	}
	return MPTCPState_AsIs
}

func (x *SocketConfig) GetTlsFragmentation() *TLSFragmentation {
	if x != nil {
		return x.TlsFragmentation
	}
	return nil
}

type TLSFragmentation struct {
	state                  protoimpl.MessageState `protogen:"open.v1"`
	TlsRecordFragmentation bool                   `protobuf:"varint,1,opt,name=tls_record_fragmentation,json=tlsRecordFragmentation,proto3" json:"tls_record_fragmentation,omitempty"`
	TcpSegmentation        bool                   `protobuf:"varint,2,opt,name=tcp_segmentation,json=tcpSegmentation,proto3" json:"tcp_segmentation,omitempty"`
	unknownFields          protoimpl.UnknownFields
	sizeCache              protoimpl.SizeCache
}

func (x *TLSFragmentation) Reset() {
	*x = TLSFragmentation{}
	mi := &file_transport_internet_config_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *TLSFragmentation) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TLSFragmentation) ProtoMessage() {}

func (x *TLSFragmentation) ProtoReflect() protoreflect.Message {
	mi := &file_transport_internet_config_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TLSFragmentation.ProtoReflect.Descriptor instead.
func (*TLSFragmentation) Descriptor() ([]byte, []int) {
	return file_transport_internet_config_proto_rawDescGZIP(), []int{4}
}

func (x *TLSFragmentation) GetTlsRecordFragmentation() bool {
	if x != nil {
		return x.TlsRecordFragmentation
	}
	return false
}

func (x *TLSFragmentation) GetTcpSegmentation() bool {
	if x != nil {
		return x.TcpSegmentation
	}
	return false
}

var File_transport_internet_config_proto protoreflect.FileDescriptor

const file_transport_internet_config_proto_rawDesc = "" +
	"\n" +
	"\x1ftransport/internet/config.proto\x12\x1fexclave.core.transport.internet\x1a\x19google/protobuf/any.proto\"n\n" +
	"\x0fTransportConfig\x12#\n" +
	"\rprotocol_name\x18\x03 \x01(\tR\fprotocolName\x120\n" +
	"\bsettings\x18\x02 \x01(\v2\x14.google.protobuf.AnyR\bsettingsJ\x04\b\x01\x10\x02\"\xda\x02\n" +
	"\fStreamConfig\x12#\n" +
	"\rprotocol_name\x18\x05 \x01(\tR\fprotocolName\x12_\n" +
	"\x12transport_settings\x18\x02 \x03(\v20.exclave.core.transport.internet.TransportConfigR\x11transportSettings\x12#\n" +
	"\rsecurity_type\x18\x03 \x01(\tR\fsecurityType\x12A\n" +
	"\x11security_settings\x18\x04 \x03(\v2\x14.google.protobuf.AnyR\x10securitySettings\x12V\n" +
	"\x0fsocket_settings\x18\x06 \x01(\v2-.exclave.core.transport.internet.SocketConfigR\x0esocketSettingsJ\x04\b\x01\x10\x02\"Q\n" +
	"\vProxyConfig\x12\x10\n" +
	"\x03tag\x18\x01 \x01(\tR\x03tag\x120\n" +
	"\x13transportLayerProxy\x18\x02 \x01(\bR\x13transportLayerProxy\"\xef\x06\n" +
	"\fSocketConfig\x12\x12\n" +
	"\x04mark\x18\x01 \x01(\rR\x04mark\x12P\n" +
	"\x03tfo\x18\x02 \x01(\x0e2>.exclave.core.transport.internet.SocketConfig.TCPFastOpenStateR\x03tfo\x12P\n" +
	"\x06tproxy\x18\x03 \x01(\x0e28.exclave.core.transport.internet.SocketConfig.TProxyModeR\x06tproxy\x12A\n" +
	"\x1dreceive_original_dest_address\x18\x04 \x01(\bR\x1areceiveOriginalDestAddress\x122\n" +
	"\x15accept_proxy_protocol\x18\a \x01(\bR\x13acceptProxyProtocol\x125\n" +
	"\x17tcp_keep_alive_interval\x18\b \x01(\x05R\x14tcpKeepAliveInterval\x12(\n" +
	"\x10tfo_queue_length\x18\t \x01(\rR\x0etfoQueueLength\x12-\n" +
	"\x13tcp_keep_alive_idle\x18\n" +
	" \x01(\x05R\x10tcpKeepAliveIdle\x12$\n" +
	"\x0ebind_to_device\x18\v \x01(\tR\fbindToDevice\x12\x1e\n" +
	"\vrx_buf_size\x18\f \x01(\x03R\trxBufSize\x12\x1e\n" +
	"\vtx_buf_size\x18\r \x01(\x03R\ttxBufSize\x12$\n" +
	"\x0eforce_buf_size\x18\x0e \x01(\bR\fforceBufSize\x12A\n" +
	"\x05mptcp\x18\x0f \x01(\x0e2+.exclave.core.transport.internet.MPTCPStateR\x05mptcp\x12]\n" +
	"\x10tlsFragmentation\x18c \x01(\v21.exclave.core.transport.internet.TLSFragmentationR\x10tlsFragmentation\"5\n" +
	"\x10TCPFastOpenState\x12\b\n" +
	"\x04AsIs\x10\x00\x12\n" +
	"\n" +
	"\x06Enable\x10\x01\x12\v\n" +
	"\aDisable\x10\x02\"/\n" +
	"\n" +
	"TProxyMode\x12\a\n" +
	"\x03Off\x10\x00\x12\n" +
	"\n" +
	"\x06TProxy\x10\x01\x12\f\n" +
	"\bRedirect\x10\x02J\x04\b\x05\x10\x06J\x04\b\x06\x10\a\"w\n" +
	"\x10TLSFragmentation\x128\n" +
	"\x18tls_record_fragmentation\x18\x01 \x01(\bR\x16tlsRecordFragmentation\x12)\n" +
	"\x10tcp_segmentation\x18\x02 \x01(\bR\x0ftcpSegmentation*/\n" +
	"\n" +
	"MPTCPState\x12\b\n" +
	"\x04AsIs\x10\x00\x12\n" +
	"\n" +
	"\x06Enable\x10\x01\x12\v\n" +
	"\aDisable\x10\x02B\x9d\x01\n" +
	"9com.github.exclavenetwork.exclave.core.transport.internetP\x01Z<github.com/exclavenetwork/exclave-core/v5/transport/internet\xaa\x02\x1fExclave.Core.Transport.Internetb\x06proto3"

var (
	file_transport_internet_config_proto_rawDescOnce sync.Once
	file_transport_internet_config_proto_rawDescData []byte
)

func file_transport_internet_config_proto_rawDescGZIP() []byte {
	file_transport_internet_config_proto_rawDescOnce.Do(func() {
		file_transport_internet_config_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_transport_internet_config_proto_rawDesc), len(file_transport_internet_config_proto_rawDesc)))
	})
	return file_transport_internet_config_proto_rawDescData
}

var file_transport_internet_config_proto_enumTypes = make([]protoimpl.EnumInfo, 3)
var file_transport_internet_config_proto_msgTypes = make([]protoimpl.MessageInfo, 5)
var file_transport_internet_config_proto_goTypes = []any{
	(MPTCPState)(0),                    // 0: exclave.core.transport.internet.MPTCPState
	(SocketConfig_TCPFastOpenState)(0), // 1: exclave.core.transport.internet.SocketConfig.TCPFastOpenState
	(SocketConfig_TProxyMode)(0),       // 2: exclave.core.transport.internet.SocketConfig.TProxyMode
	(*TransportConfig)(nil),            // 3: exclave.core.transport.internet.TransportConfig
	(*StreamConfig)(nil),               // 4: exclave.core.transport.internet.StreamConfig
	(*ProxyConfig)(nil),                // 5: exclave.core.transport.internet.ProxyConfig
	(*SocketConfig)(nil),               // 6: exclave.core.transport.internet.SocketConfig
	(*TLSFragmentation)(nil),           // 7: exclave.core.transport.internet.TLSFragmentation
	(*anypb.Any)(nil),                  // 8: google.protobuf.Any
}
var file_transport_internet_config_proto_depIdxs = []int32{
	8, // 0: exclave.core.transport.internet.TransportConfig.settings:type_name -> google.protobuf.Any
	3, // 1: exclave.core.transport.internet.StreamConfig.transport_settings:type_name -> exclave.core.transport.internet.TransportConfig
	8, // 2: exclave.core.transport.internet.StreamConfig.security_settings:type_name -> google.protobuf.Any
	6, // 3: exclave.core.transport.internet.StreamConfig.socket_settings:type_name -> exclave.core.transport.internet.SocketConfig
	1, // 4: exclave.core.transport.internet.SocketConfig.tfo:type_name -> exclave.core.transport.internet.SocketConfig.TCPFastOpenState
	2, // 5: exclave.core.transport.internet.SocketConfig.tproxy:type_name -> exclave.core.transport.internet.SocketConfig.TProxyMode
	0, // 6: exclave.core.transport.internet.SocketConfig.mptcp:type_name -> exclave.core.transport.internet.MPTCPState
	7, // 7: exclave.core.transport.internet.SocketConfig.tlsFragmentation:type_name -> exclave.core.transport.internet.TLSFragmentation
	8, // [8:8] is the sub-list for method output_type
	8, // [8:8] is the sub-list for method input_type
	8, // [8:8] is the sub-list for extension type_name
	8, // [8:8] is the sub-list for extension extendee
	0, // [0:8] is the sub-list for field type_name
}

func init() { file_transport_internet_config_proto_init() }
func file_transport_internet_config_proto_init() {
	if File_transport_internet_config_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_transport_internet_config_proto_rawDesc), len(file_transport_internet_config_proto_rawDesc)),
			NumEnums:      3,
			NumMessages:   5,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_transport_internet_config_proto_goTypes,
		DependencyIndexes: file_transport_internet_config_proto_depIdxs,
		EnumInfos:         file_transport_internet_config_proto_enumTypes,
		MessageInfos:      file_transport_internet_config_proto_msgTypes,
	}.Build()
	File_transport_internet_config_proto = out.File
	file_transport_internet_config_proto_goTypes = nil
	file_transport_internet_config_proto_depIdxs = nil
}
