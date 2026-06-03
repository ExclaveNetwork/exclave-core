package hysteria2

import (
	protocol "github.com/exclavenetwork/exclave-core/v5/common/protocol"
	_ "github.com/exclavenetwork/exclave-core/v5/common/protoext"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
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

type Account struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Account) Reset() {
	*x = Account{}
	mi := &file_proxy_hysteria2_config_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Account) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Account) ProtoMessage() {}

func (x *Account) ProtoReflect() protoreflect.Message {
	mi := &file_proxy_hysteria2_config_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Account.ProtoReflect.Descriptor instead.
func (*Account) Descriptor() ([]byte, []int) {
	return file_proxy_hysteria2_config_proto_rawDescGZIP(), []int{0}
}

type ClientConfig struct {
	state         protoimpl.MessageState     `protogen:"open.v1"`
	Server        []*protocol.ServerEndpoint `protobuf:"bytes,1,rep,name=server,proto3" json:"server,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ClientConfig) Reset() {
	*x = ClientConfig{}
	mi := &file_proxy_hysteria2_config_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ClientConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ClientConfig) ProtoMessage() {}

func (x *ClientConfig) ProtoReflect() protoreflect.Message {
	mi := &file_proxy_hysteria2_config_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ClientConfig.ProtoReflect.Descriptor instead.
func (*ClientConfig) Descriptor() ([]byte, []int) {
	return file_proxy_hysteria2_config_proto_rawDescGZIP(), []int{1}
}

func (x *ClientConfig) GetServer() []*protocol.ServerEndpoint {
	if x != nil {
		return x.Server
	}
	return nil
}

type ServerConfig struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ServerConfig) Reset() {
	*x = ServerConfig{}
	mi := &file_proxy_hysteria2_config_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServerConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServerConfig) ProtoMessage() {}

func (x *ServerConfig) ProtoReflect() protoreflect.Message {
	mi := &file_proxy_hysteria2_config_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServerConfig.ProtoReflect.Descriptor instead.
func (*ServerConfig) Descriptor() ([]byte, []int) {
	return file_proxy_hysteria2_config_proto_rawDescGZIP(), []int{2}
}

var File_proxy_hysteria2_config_proto protoreflect.FileDescriptor

const file_proxy_hysteria2_config_proto_rawDesc = "" +
	"\n" +
	"\x1cproxy/hysteria2/config.proto\x12\x1cexclave.core.proxy.hysteria2\x1a!common/protocol/server_spec.proto\x1a common/protoext/extensions.proto\"\t\n" +
	"\aAccount\"o\n" +
	"\fClientConfig\x12D\n" +
	"\x06server\x18\x01 \x03(\v2,.exclave.core.common.protocol.ServerEndpointR\x06server:\x19\x82\xb5\x18\x15\n" +
	"\boutbound\x12\thysteria2\".\n" +
	"\fServerConfig:\x18\x82\xb5\x18\x14\n" +
	"\ainbound\x12\thysteria2J\x04\b\x01\x10\x02B\x94\x01\n" +
	"6com.github.exclavenetwork.exclave.core.proxy.hysteria2P\x01Z9github.com/exclavenetwork/exclave-core/v5/proxy/hysteria2\xaa\x02\x1cExclave.Core.Proxy.Hysteria2b\x06proto3"

var (
	file_proxy_hysteria2_config_proto_rawDescOnce sync.Once
	file_proxy_hysteria2_config_proto_rawDescData []byte
)

func file_proxy_hysteria2_config_proto_rawDescGZIP() []byte {
	file_proxy_hysteria2_config_proto_rawDescOnce.Do(func() {
		file_proxy_hysteria2_config_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_proxy_hysteria2_config_proto_rawDesc), len(file_proxy_hysteria2_config_proto_rawDesc)))
	})
	return file_proxy_hysteria2_config_proto_rawDescData
}

var file_proxy_hysteria2_config_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_proxy_hysteria2_config_proto_goTypes = []any{
	(*Account)(nil),                 // 0: exclave.core.proxy.hysteria2.Account
	(*ClientConfig)(nil),            // 1: exclave.core.proxy.hysteria2.ClientConfig
	(*ServerConfig)(nil),            // 2: exclave.core.proxy.hysteria2.ServerConfig
	(*protocol.ServerEndpoint)(nil), // 3: exclave.core.common.protocol.ServerEndpoint
}
var file_proxy_hysteria2_config_proto_depIdxs = []int32{
	3, // 0: exclave.core.proxy.hysteria2.ClientConfig.server:type_name -> exclave.core.common.protocol.ServerEndpoint
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_proxy_hysteria2_config_proto_init() }
func file_proxy_hysteria2_config_proto_init() {
	if File_proxy_hysteria2_config_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_proxy_hysteria2_config_proto_rawDesc), len(file_proxy_hysteria2_config_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_proxy_hysteria2_config_proto_goTypes,
		DependencyIndexes: file_proxy_hysteria2_config_proto_depIdxs,
		MessageInfos:      file_proxy_hysteria2_config_proto_msgTypes,
	}.Build()
	File_proxy_hysteria2_config_proto = out.File
	file_proxy_hysteria2_config_proto_goTypes = nil
	file_proxy_hysteria2_config_proto_depIdxs = nil
}
