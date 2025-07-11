// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: kannon/stats/apiv1/statsapiv1.proto

package apiv1

import (
	types "github.com/kannon-email/kannon/proto/kannon/stats/types"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
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

type GetStatsReq struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Domain        string                 `protobuf:"bytes,1,opt,name=domain,proto3" json:"domain,omitempty"`
	FromDate      *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=from_date,json=fromDate,proto3" json:"from_date,omitempty"`
	ToDate        *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=to_date,json=toDate,proto3" json:"to_date,omitempty"`
	Skip          uint32                 `protobuf:"varint,4,opt,name=skip,proto3" json:"skip,omitempty"`
	Take          uint32                 `protobuf:"varint,5,opt,name=take,proto3" json:"take,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetStatsReq) Reset() {
	*x = GetStatsReq{}
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetStatsReq) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetStatsReq) ProtoMessage() {}

func (x *GetStatsReq) ProtoReflect() protoreflect.Message {
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetStatsReq.ProtoReflect.Descriptor instead.
func (*GetStatsReq) Descriptor() ([]byte, []int) {
	return file_kannon_stats_apiv1_statsapiv1_proto_rawDescGZIP(), []int{0}
}

func (x *GetStatsReq) GetDomain() string {
	if x != nil {
		return x.Domain
	}
	return ""
}

func (x *GetStatsReq) GetFromDate() *timestamppb.Timestamp {
	if x != nil {
		return x.FromDate
	}
	return nil
}

func (x *GetStatsReq) GetToDate() *timestamppb.Timestamp {
	if x != nil {
		return x.ToDate
	}
	return nil
}

func (x *GetStatsReq) GetSkip() uint32 {
	if x != nil {
		return x.Skip
	}
	return 0
}

func (x *GetStatsReq) GetTake() uint32 {
	if x != nil {
		return x.Take
	}
	return 0
}

type GetStatsRes struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Total         uint32                 `protobuf:"varint,1,opt,name=total,proto3" json:"total,omitempty"`
	Stats         []*types.Stats         `protobuf:"bytes,2,rep,name=stats,proto3" json:"stats,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetStatsRes) Reset() {
	*x = GetStatsRes{}
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetStatsRes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetStatsRes) ProtoMessage() {}

func (x *GetStatsRes) ProtoReflect() protoreflect.Message {
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetStatsRes.ProtoReflect.Descriptor instead.
func (*GetStatsRes) Descriptor() ([]byte, []int) {
	return file_kannon_stats_apiv1_statsapiv1_proto_rawDescGZIP(), []int{1}
}

func (x *GetStatsRes) GetTotal() uint32 {
	if x != nil {
		return x.Total
	}
	return 0
}

func (x *GetStatsRes) GetStats() []*types.Stats {
	if x != nil {
		return x.Stats
	}
	return nil
}

type GetStatsAggregatedReq struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Domain        string                 `protobuf:"bytes,1,opt,name=domain,proto3" json:"domain,omitempty"`
	FromDate      *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=from_date,json=fromDate,proto3" json:"from_date,omitempty"`
	ToDate        *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=to_date,json=toDate,proto3" json:"to_date,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetStatsAggregatedReq) Reset() {
	*x = GetStatsAggregatedReq{}
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetStatsAggregatedReq) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetStatsAggregatedReq) ProtoMessage() {}

func (x *GetStatsAggregatedReq) ProtoReflect() protoreflect.Message {
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetStatsAggregatedReq.ProtoReflect.Descriptor instead.
func (*GetStatsAggregatedReq) Descriptor() ([]byte, []int) {
	return file_kannon_stats_apiv1_statsapiv1_proto_rawDescGZIP(), []int{2}
}

func (x *GetStatsAggregatedReq) GetDomain() string {
	if x != nil {
		return x.Domain
	}
	return ""
}

func (x *GetStatsAggregatedReq) GetFromDate() *timestamppb.Timestamp {
	if x != nil {
		return x.FromDate
	}
	return nil
}

func (x *GetStatsAggregatedReq) GetToDate() *timestamppb.Timestamp {
	if x != nil {
		return x.ToDate
	}
	return nil
}

type GetStatsAggregatedRes struct {
	state         protoimpl.MessageState   `protogen:"open.v1"`
	Stats         []*types.StatsAggregated `protobuf:"bytes,1,rep,name=stats,proto3" json:"stats,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetStatsAggregatedRes) Reset() {
	*x = GetStatsAggregatedRes{}
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetStatsAggregatedRes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetStatsAggregatedRes) ProtoMessage() {}

func (x *GetStatsAggregatedRes) ProtoReflect() protoreflect.Message {
	mi := &file_kannon_stats_apiv1_statsapiv1_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetStatsAggregatedRes.ProtoReflect.Descriptor instead.
func (*GetStatsAggregatedRes) Descriptor() ([]byte, []int) {
	return file_kannon_stats_apiv1_statsapiv1_proto_rawDescGZIP(), []int{3}
}

func (x *GetStatsAggregatedRes) GetStats() []*types.StatsAggregated {
	if x != nil {
		return x.Stats
	}
	return nil
}

var File_kannon_stats_apiv1_statsapiv1_proto protoreflect.FileDescriptor

const file_kannon_stats_apiv1_statsapiv1_proto_rawDesc = "" +
	"\n" +
	"#kannon/stats/apiv1/statsapiv1.proto\x12\x06kannon\x1a\x1fgoogle/protobuf/timestamp.proto\x1a\x1ekannon/stats/types/stats.proto\"\xbb\x01\n" +
	"\vGetStatsReq\x12\x16\n" +
	"\x06domain\x18\x01 \x01(\tR\x06domain\x127\n" +
	"\tfrom_date\x18\x02 \x01(\v2\x1a.google.protobuf.TimestampR\bfromDate\x123\n" +
	"\ato_date\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\x06toDate\x12\x12\n" +
	"\x04skip\x18\x04 \x01(\rR\x04skip\x12\x12\n" +
	"\x04take\x18\x05 \x01(\rR\x04take\"X\n" +
	"\vGetStatsRes\x12\x14\n" +
	"\x05total\x18\x01 \x01(\rR\x05total\x123\n" +
	"\x05stats\x18\x02 \x03(\v2\x1d.pkg.kannon.stats.types.StatsR\x05stats\"\x9d\x01\n" +
	"\x15GetStatsAggregatedReq\x12\x16\n" +
	"\x06domain\x18\x01 \x01(\tR\x06domain\x127\n" +
	"\tfrom_date\x18\x02 \x01(\v2\x1a.google.protobuf.TimestampR\bfromDate\x123\n" +
	"\ato_date\x18\x03 \x01(\v2\x1a.google.protobuf.TimestampR\x06toDate\"V\n" +
	"\x15GetStatsAggregatedRes\x12=\n" +
	"\x05stats\x18\x01 \x03(\v2'.pkg.kannon.stats.types.StatsAggregatedR\x05stats2\x9a\x01\n" +
	"\n" +
	"StatsApiV1\x126\n" +
	"\bGetStats\x12\x13.kannon.GetStatsReq\x1a\x13.kannon.GetStatsRes\"\x00\x12T\n" +
	"\x12GetStatsAggregated\x12\x1d.kannon.GetStatsAggregatedReq\x1a\x1d.kannon.GetStatsAggregatedRes\"\x00B\x8e\x01\n" +
	"\n" +
	"com.kannonB\x0fStatsapiv1ProtoP\x01Z7github.com/kannon-email/kannon/proto/kannon/stats/apiv1\xa2\x02\x03KXX\xaa\x02\x06Kannon\xca\x02\x06Kannon\xe2\x02\x12Kannon\\GPBMetadata\xea\x02\x06Kannonb\x06proto3"

var (
	file_kannon_stats_apiv1_statsapiv1_proto_rawDescOnce sync.Once
	file_kannon_stats_apiv1_statsapiv1_proto_rawDescData []byte
)

func file_kannon_stats_apiv1_statsapiv1_proto_rawDescGZIP() []byte {
	file_kannon_stats_apiv1_statsapiv1_proto_rawDescOnce.Do(func() {
		file_kannon_stats_apiv1_statsapiv1_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_kannon_stats_apiv1_statsapiv1_proto_rawDesc), len(file_kannon_stats_apiv1_statsapiv1_proto_rawDesc)))
	})
	return file_kannon_stats_apiv1_statsapiv1_proto_rawDescData
}

var file_kannon_stats_apiv1_statsapiv1_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_kannon_stats_apiv1_statsapiv1_proto_goTypes = []any{
	(*GetStatsReq)(nil),           // 0: kannon.GetStatsReq
	(*GetStatsRes)(nil),           // 1: kannon.GetStatsRes
	(*GetStatsAggregatedReq)(nil), // 2: kannon.GetStatsAggregatedReq
	(*GetStatsAggregatedRes)(nil), // 3: kannon.GetStatsAggregatedRes
	(*timestamppb.Timestamp)(nil), // 4: google.protobuf.Timestamp
	(*types.Stats)(nil),           // 5: pkg.kannon.stats.types.Stats
	(*types.StatsAggregated)(nil), // 6: pkg.kannon.stats.types.StatsAggregated
}
var file_kannon_stats_apiv1_statsapiv1_proto_depIdxs = []int32{
	4, // 0: kannon.GetStatsReq.from_date:type_name -> google.protobuf.Timestamp
	4, // 1: kannon.GetStatsReq.to_date:type_name -> google.protobuf.Timestamp
	5, // 2: kannon.GetStatsRes.stats:type_name -> pkg.kannon.stats.types.Stats
	4, // 3: kannon.GetStatsAggregatedReq.from_date:type_name -> google.protobuf.Timestamp
	4, // 4: kannon.GetStatsAggregatedReq.to_date:type_name -> google.protobuf.Timestamp
	6, // 5: kannon.GetStatsAggregatedRes.stats:type_name -> pkg.kannon.stats.types.StatsAggregated
	0, // 6: kannon.StatsApiV1.GetStats:input_type -> kannon.GetStatsReq
	2, // 7: kannon.StatsApiV1.GetStatsAggregated:input_type -> kannon.GetStatsAggregatedReq
	1, // 8: kannon.StatsApiV1.GetStats:output_type -> kannon.GetStatsRes
	3, // 9: kannon.StatsApiV1.GetStatsAggregated:output_type -> kannon.GetStatsAggregatedRes
	8, // [8:10] is the sub-list for method output_type
	6, // [6:8] is the sub-list for method input_type
	6, // [6:6] is the sub-list for extension type_name
	6, // [6:6] is the sub-list for extension extendee
	0, // [0:6] is the sub-list for field type_name
}

func init() { file_kannon_stats_apiv1_statsapiv1_proto_init() }
func file_kannon_stats_apiv1_statsapiv1_proto_init() {
	if File_kannon_stats_apiv1_statsapiv1_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_kannon_stats_apiv1_statsapiv1_proto_rawDesc), len(file_kannon_stats_apiv1_statsapiv1_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_kannon_stats_apiv1_statsapiv1_proto_goTypes,
		DependencyIndexes: file_kannon_stats_apiv1_statsapiv1_proto_depIdxs,
		MessageInfos:      file_kannon_stats_apiv1_statsapiv1_proto_msgTypes,
	}.Build()
	File_kannon_stats_apiv1_statsapiv1_proto = out.File
	file_kannon_stats_apiv1_statsapiv1_proto_goTypes = nil
	file_kannon_stats_apiv1_statsapiv1_proto_depIdxs = nil
}
