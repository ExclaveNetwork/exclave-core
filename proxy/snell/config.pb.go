package snell

import (
	net "github.com/exclavenetwork/exclave-core/v5/common/net"
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

type ClientConfig struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Address       *net.IPOrDomain        `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
	Port          uint32                 `protobuf:"varint,2,opt,name=port,proto3" json:"port,omitempty"`
	Psk           string                 `protobuf:"bytes,3,opt,name=psk,proto3" json:"psk,omitempty"`
	Version       uint32                 `protobuf:"varint,4,opt,name=version,proto3" json:"version,omitempty"`
	UserKey       string                 `protobuf:"bytes,5,opt,name=user_key,json=userKey,proto3" json:"user_key,omitempty"`
	Reuse         bool                   `protobuf:"varint,6,opt,name=reuse,proto3" json:"reuse,omitempty"`
	ObfsMode      string                 `protobuf:"bytes,7,opt,name=obfs_mode,json=obfsMode,proto3" json:"obfs_mode,omitempty"`
	ObfsHost      string                 `protobuf:"bytes,8,opt,name=obfs_host,json=obfsHost,proto3" json:"obfs_host,omitempty"`
	Mode          string                 `protobuf:"bytes,9,opt,name=mode,proto3" json:"mode,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ClientConfig) Reset() {
	*x = ClientConfig{}
	mi := &file_proxy_snell_config_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ClientConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ClientConfig) ProtoMessage() {}

func (x *ClientConfig) ProtoReflect() protoreflect.Message {
	mi := &file_proxy_snell_config_proto_msgTypes[0]
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
	return file_proxy_snell_config_proto_rawDescGZIP(), []int{0}
}

func (x *ClientConfig) GetAddress() *net.IPOrDomain {
	if x != nil {
		return x.Address
	}
	return nil
}

func (x *ClientConfig) GetPort() uint32 {
	if x != nil {
		return x.Port
	}
	return 0
}

func (x *ClientConfig) GetPsk() string {
	if x != nil {
		return x.Psk
	}
	return ""
}

func (x *ClientConfig) GetVersion() uint32 {
	if x != nil {
		return x.Version
	}
	return 0
}

func (x *ClientConfig) GetUserKey() string {
	if x != nil {
		return x.UserKey
	}
	return ""
}

func (x *ClientConfig) GetReuse() bool {
	if x != nil {
		return x.Reuse
	}
	return false
}

func (x *ClientConfig) GetObfsMode() string {
	if x != nil {
		return x.ObfsMode
	}
	return ""
}

func (x *ClientConfig) GetObfsHost() string {
	if x != nil {
		return x.ObfsHost
	}
	return ""
}

func (x *ClientConfig) GetMode() string {
	if x != nil {
		return x.Mode
	}
	return ""
}

var File_proxy_snell_config_proto protoreflect.FileDescriptor

const file_proxy_snell_config_proto_rawDesc = "" +
	"\n" +
	"\x18proxy/snell/config.proto\x12\x18exclave.core.proxy.snell\x1a common/protoext/extensions.proto\x1a\x18common/net/address.proto\"\xa3\x02\n" +
	"\fClientConfig\x12=\n" +
	"\aaddress\x18\x01 \x01(\v2#.exclave.core.common.net.IPOrDomainR\aaddress\x12\x12\n" +
	"\x04port\x18\x02 \x01(\rR\x04port\x12\x10\n" +
	"\x03psk\x18\x03 \x01(\tR\x03psk\x12\x18\n" +
	"\aversion\x18\x04 \x01(\rR\aversion\x12\x19\n" +
	"\buser_key\x18\x05 \x01(\tR\auserKey\x12\x14\n" +
	"\x05reuse\x18\x06 \x01(\bR\x05reuse\x12\x1b\n" +
	"\tobfs_mode\x18\a \x01(\tR\bobfsMode\x12\x1b\n" +
	"\tobfs_host\x18\b \x01(\tR\bobfsHost\x12\x12\n" +
	"\x04mode\x18\t \x01(\tR\x04mode:\x15\x82\xb5\x18\x11\n" +
	"\boutbound\x12\x05snellB\x88\x01\n" +
	"2com.github.exclavenetwork.exclave.core.proxy.snellP\x01Z5github.com/exclavenetwork/exclave-core/v5/proxy/snell\xaa\x02\x18Exclave.Core.Proxy.Snellb\x06proto3"

var (
	file_proxy_snell_config_proto_rawDescOnce sync.Once
	file_proxy_snell_config_proto_rawDescData []byte
)

func file_proxy_snell_config_proto_rawDescGZIP() []byte {
	file_proxy_snell_config_proto_rawDescOnce.Do(func() {
		file_proxy_snell_config_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_proxy_snell_config_proto_rawDesc), len(file_proxy_snell_config_proto_rawDesc)))
	})
	return file_proxy_snell_config_proto_rawDescData
}

var file_proxy_snell_config_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_proxy_snell_config_proto_goTypes = []any{
	(*ClientConfig)(nil),   // 0: exclave.core.proxy.snell.ClientConfig
	(*net.IPOrDomain)(nil), // 1: exclave.core.common.net.IPOrDomain
}
var file_proxy_snell_config_proto_depIdxs = []int32{
	1, // 0: exclave.core.proxy.snell.ClientConfig.address:type_name -> exclave.core.common.net.IPOrDomain
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_proxy_snell_config_proto_init() }
func file_proxy_snell_config_proto_init() {
	if File_proxy_snell_config_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_proxy_snell_config_proto_rawDesc), len(file_proxy_snell_config_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_proxy_snell_config_proto_goTypes,
		DependencyIndexes: file_proxy_snell_config_proto_depIdxs,
		MessageInfos:      file_proxy_snell_config_proto_msgTypes,
	}.Build()
	File_proxy_snell_config_proto = out.File
	file_proxy_snell_config_proto_goTypes = nil
	file_proxy_snell_config_proto_depIdxs = nil
}
