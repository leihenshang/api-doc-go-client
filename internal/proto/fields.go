package proto

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// FieldInfo 入参消息的顶层字段（Message 分段的字段提示，G5.4）。
//
// JSONName 是写请求消息时要用的键：protojson 同时认 proto 名与 JSON 名，
// 但生成的样例用 JSON 名（`user_id` → `userId`），界面统一提示 JSON 名以免两边写法混用。
type FieldInfo struct {
	Name     string `json:"name"`     // proto 名：user_id
	JSONName string `json:"jsonName"` // protojson 名：userId
	Type     string `json:"type"`     // 展示形态：string / repeated string / demo.Nested / map<string, string>
	Kind     string `json:"kind"`     // scalar | message | enum | map
	Repeated bool   `json:"repeated"`
	Comment  string `json:"comment"` // proto 注释（可为空）
}

// Fields 列出入参消息的顶层字段（保持 proto 里的声明顺序）。
// oneof 成员照常列出（protojson 里按成员名写即可），不做折叠。
func Fields(md protoreflect.MessageDescriptor) []FieldInfo {
	if md == nil {
		return nil
	}
	fields := md.Fields()
	out := make([]FieldInfo, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		out = append(out, FieldInfo{
			Name:     string(fd.Name()),
			JSONName: fd.JSONName(),
			Type:     fieldType(fd),
			Kind:     fieldKind(fd),
			Repeated: fd.IsList(),
			Comment:  commentOf(fd),
		})
	}
	return out
}

// ValidateMessage 校验 protojson 请求消息（Message 分段的内联提示，G5.3）。
//
// 空文本视为通过：gRPC 允许发送「全默认值」的消息（执行器同样跳过解析），
// 界面不应该把「没填内容」当成错误。未知字段不忽略（DiscardUnknown=false），
// 这样拼错字段名能立刻暴露，错误文案自带字段路径。
func ValidateMessage(md protoreflect.MethodDescriptor, text string, types *protoregistry.Types) error {
	if md == nil {
		return errors.New("缺少方法描述符")
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	msg := dynamicpb.NewMessage(md.Input())
	opts := protojson.UnmarshalOptions{Resolver: types}
	if err := opts.Unmarshal([]byte(text), msg); err != nil {
		return err
	}
	return nil
}

// fieldKind 字段类别：map / message / enum / scalar。
// List 字段的 Kind() 返回的是元素类型，故 repeated message 也会判成 message。
func fieldKind(fd protoreflect.FieldDescriptor) string {
	if fd.IsMap() {
		return "map"
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "message"
	case protoreflect.EnumKind:
		return "enum"
	default:
		return "scalar"
	}
}

// fieldType 展示形态：map 展开键值、repeated 加前缀、消息 / 枚举给全名、标量给 Kind 名。
func fieldType(fd protoreflect.FieldDescriptor) string {
	if fd.IsMap() {
		return fmt.Sprintf("map<%s, %s>", fd.MapKey().Kind().String(), elementName(fd.MapValue()))
	}
	name := elementName(fd)
	if fd.IsList() {
		return "repeated " + name
	}
	return name
}

// elementName 元素层的类型名（消息 / 枚举用全名，其余用 Kind 名，如 string / int32 / bytes）。
func elementName(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if md := fd.Message(); md != nil {
			return string(md.FullName())
		}
		return "message"
	case protoreflect.EnumKind:
		if ed := fd.Enum(); ed != nil {
			return string(ed.FullName())
		}
		return "enum"
	default:
		return fd.Kind().String()
	}
}
