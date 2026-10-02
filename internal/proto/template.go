package proto

import (
	"errors"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// sampleMaxDepth 样例消息递归上限：自引用消息（树/链式结构）不能无限展开。
const sampleMaxDepth = 3

// SampleMessage 按描述符生成一份可编辑的请求样例（protojson，缩进 2）。
//
// 规则：标量取类型默认值；repeated / map 各给一条；oneof 只给第一个成员；
// 嵌套消息递归到 sampleMaxDepth 层；google.protobuf.Any 需要 @type 才能编码，故留空。
// types 可为 nil（无需解析 Any / 扩展时）。
func SampleMessage(md protoreflect.MessageDescriptor, types *protoregistry.Types) (string, error) {
	if md == nil {
		return "", errors.New("缺少消息描述符")
	}
	msg := sampleMessage(md, 0)
	opts := protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}
	if types != nil {
		opts.Resolver = types
	}
	out, err := opts.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// sampleMessage 递归构造样例消息。
func sampleMessage(md protoreflect.MessageDescriptor, depth int) *dynamicpb.Message {
	msg := dynamicpb.NewMessage(md)
	fields := md.Fields()
	doneOneof := map[protoreflect.FullName]bool{}

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
			if doneOneof[od.FullName()] {
				continue // oneof 只能给一个成员
			}
			doneOneof[od.FullName()] = true
		}
		switch {
		case fd.IsMap():
			// map 字段本身是「合成 entry 消息」，样例值要按 value 字段算（否则类型不匹配）
			valueField := fd.MapValue()
			if isAny(valueField) {
				continue
			}
			value, ok := sampleValue(valueField, depth)
			if !ok {
				continue
			}
			msg.Mutable(fd).Map().Set(sampleMapKey(fd.MapKey()), value)
		case fd.IsList():
			if isAny(fd) {
				continue
			}
			value, ok := sampleValue(fd, depth)
			if !ok {
				continue
			}
			msg.Mutable(fd).List().Append(value)
		default:
			if isAny(fd) {
				continue // google.protobuf.Any 需要 @type 才能编码，留空
			}
			value, ok := sampleValue(fd, depth)
			if !ok {
				continue
			}
			msg.Set(fd, value)
		}
	}
	return msg
}

// isAny 判断字段是否是 google.protobuf.Any（无法自动编造内容）。
func isAny(fd protoreflect.FieldDescriptor) bool {
	return fd.Message() != nil && fd.Message().FullName() == "google.protobuf.Any"
}

// sampleValue 单个字段的样例值；ok=false 表示该字段留空（例如超出递归深度）。
func sampleValue(fd protoreflect.FieldDescriptor, depth int) (protoreflect.Value, bool) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(false), true
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(0), true
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(0), true
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(0), true
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(0), true
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(0), true
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(0), true
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(""), true
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes(nil), true
	case protoreflect.EnumKind:
		values := fd.Enum().Values()
		if values.Len() == 0 {
			return protoreflect.Value{}, false
		}
		return protoreflect.ValueOfEnum(values.Get(0).Number()), true
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if depth >= sampleMaxDepth {
			return protoreflect.Value{}, false
		}
		return protoreflect.ValueOfMessage(sampleMessage(fd.Message(), depth+1)), true
	default:
		return protoreflect.Value{}, false
	}
}

// sampleMapKey map 字段的样例键（按键类型给一个安全值）。
func sampleMapKey(fd protoreflect.FieldDescriptor) protoreflect.MapKey {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(false).MapKey()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(0).MapKey()
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(0).MapKey()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(0).MapKey()
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(0).MapKey()
	default:
		return protoreflect.ValueOfString("key").MapKey()
	}
}
