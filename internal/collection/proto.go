package collection

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// protosDir 导入的 .proto 落点（相对集合目录），与 docs/ 同级约定。
// 放进集合是为了：自包含、可 git 提交、跨机一致（设计文档 D2）。
const protosDir = "protos"

// protoExt 允许导入的定义文件类型。
const protoExt = ".proto"

// ProtoFileInfo 集合内一个已导入定义的信息。
type ProtoFileInfo struct {
	Rel  string `json:"rel"`  // 相对集合目录：protos/greeter.proto
	Size int64  `json:"size"` // 字节数
	Mod  int64  `json:"mod"`  // 导入时间（Unix 秒）
}

// ProtoDir 定义目录（绝对路径）：编译时默认加进 import 搜索路径。
func (c *Collection) ProtoDir() string { return filepath.Join(c.Dir, protosDir) }

// ImportProtos 把外部 .proto 复制进集合 protos/ 目录（导入即落盘，设计文档 G2.1/D2）。
//
// 规则：
//   - 只接受 .proto；同名文件**覆盖**（这就是「更新定义」的路径，D3：更新只由用户显式触发）；
//   - 同一次导入里选中多个同名文件时，第二个起追加序号（greeter-2.proto），避免互相覆盖；
//   - 返回落盘后的相对路径，调用方把它写进请求的 grpc.proto。
func (c *Collection) ImportProtos(files []string) ([]ProtoFileInfo, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("没有选择 .proto 文件")
	}
	if err := os.MkdirAll(c.ProtoDir(), 0o755); err != nil {
		return nil, fmt.Errorf("创建 %s 目录失败: %w", protosDir, err)
	}
	used := map[string]bool{}
	out := make([]ProtoFileInfo, 0, len(files))
	for _, src := range files {
		info, err := c.importOneProto(src, used)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// importOneProto 复制单个文件；used 记录本次导入已占用的名字（同名加序号）。
func (c *Collection) importOneProto(src string, used map[string]bool) (ProtoFileInfo, error) {
	if strings.ToLower(filepath.Ext(src)) != protoExt {
		return ProtoFileInfo{}, fmt.Errorf("只支持导入 %s 文件: %s", protoExt, src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return ProtoFileInfo{}, fmt.Errorf("读取 %s 失败: %w", src, err)
	}
	base := filepath.Base(src)
	name := base
	for i := 2; used[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, protoExt), i, protoExt)
	}
	used[strings.ToLower(name)] = true

	rel := protosDir + "/" + name
	dst := filepath.Join(c.Dir, protosDir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return ProtoFileInfo{}, fmt.Errorf("写入 %s 失败: %w", rel, err)
	}
	// 自写回环：.proto 本就不在监听白名单里，这里再兜一层，防止将来白名单变化后自触发
	c.ignoreWrite(rel)
	return ProtoFileInfo{Rel: rel, Size: int64(len(data)), Mod: time.Now().Unix()}, nil
}

// ListProtos 列出集合里已导入的定义（递归；供「从集合里选定义」的下拉，免点文件选择器）。
func (c *Collection) ListProtos() ([]ProtoFileInfo, error) {
	root := c.ProtoDir()
	out := []ProtoFileInfo{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // 目录还不存在：当作空列表
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != protoExt {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(c.Dir, path)
		if rerr != nil {
			return nil
		}
		out = append(out, ProtoFileInfo{
			Rel:  filepath.ToSlash(rel),
			Size: info.Size(),
			Mod:  info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// RemoveProto 删除集合内的一个定义文件（refs 为引用它的请求数，由调用方先查）。
func (c *Collection) RemoveProto(rel string) error {
	full, err := c.protoPath(rel)
	if err != nil {
		return err
	}
	if _, serr := os.Stat(full); serr != nil {
		return fmt.Errorf("定义不存在: %s", rel)
	}
	c.ignoreWrite(filepath.ToSlash(rel))
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("删除 %s 失败: %w", rel, err)
	}
	return nil
}

// ProtoPath 把集合内的相对定义路径解析成绝对路径（并挡住越界路径）。
func (c *Collection) ProtoPath(rel string) (string, error) { return c.protoPath(rel) }

// protoPath 校验 rel 必须落在 protos/ 目录内。
func (c *Collection) protoPath(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) {
		return "", fmt.Errorf("定义路径不合法: %s", rel)
	}
	full := filepath.Join(c.Dir, clean)
	base := filepath.Clean(c.ProtoDir())
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("定义必须位于 %s 目录内: %s", protosDir, rel)
	}
	return full, nil
}
