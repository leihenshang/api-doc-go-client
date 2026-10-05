package mcp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	sharecoll "github.com/leihenshang/api-doc-go-share/collection"
	sharevarx "github.com/leihenshang/api-doc-go-share/varx"

	"api-doc-go-client/internal/collection"
)

// 环境变量管理（增删改查）—— MCP 工具层门面。
//
// 落盘由 collection 层负责：环境 = environments/<env>.yml，敏感变量的值另存
// <env>.secrets.yml（SaveEnv 自动拆分、ListEnvs 读回时自动合并），删除进 .trash/。
// 这里只做「定位项目 → 读写环境 → 给出可读结论」，并守住三条对外约定：
//
//  1. 敏感变量（secret=true）的值**一律掩码**返回，不把真实密钥交给 AI；
//  2. set_env_var 收到掩码值时视为「这条密钥保持不变」——否则 AI 把 list_envs 的输出
//     原样回写，就会把 "••••••" 当成真密钥存进去，密钥直接废掉；
//  3. 变量名按占位符语法校验（见 varNamePattern），不建「永远无法被 {{name}} 引用」的变量；
//     环境名沿用共享包的 ValidEnvName，并额外拒绝分隔符与 . / ..，避免越出集合目录。

// maskedValue 敏感变量对外展示的掩码。取自共享包，界面与 MCP 侧不会漂移。
const maskedValue = sharevarx.MaskedValue

// varNamePattern 合法变量名：与共享包占位符语法 {{name}} 保持一致（渲染阶段的正则是
// \{\{\s*(\$?[A-Za-z_][A-Za-z0-9_]*)\s*\}\}），所以这里的规则不能再放宽。
var varNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var (
	errNeedEnvName = errors.New("环境名不能为空")
	errNeedVarName = errors.New("变量名不能为空")
)

// EnvVarEntry 一个环境变量条目。Secret 为 true 时 Value 只是掩码，不是真值。
type EnvVarEntry struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Secret  bool   `json:"secret"`
	Enabled bool   `json:"enabled"`
	// KeptSecret 本次调用没有改动密钥（传入的是掩码或纯占位字符）。
	// 让工具结果能说清「到底改没改」，否则 AI 拿到一个掩码会以为密钥被改成了掩码。
	KeptSecret bool `json:"keptSecret,omitempty"`
	// Hash 所属环境文件在本次写入后的内容哈希，可作为下一次 set_env_var /
	// delete_env_var / rename_env 的 ifMatch（连续操作时不必重新 list_envs）。
	Hash string `json:"hash,omitempty"`
}

// EnvEntry 一个环境的完整内容（变量名 -> 条目）。
type EnvEntry struct {
	Name    string                 `json:"name"`
	Default bool                   `json:"default"`
	Vars    map[string]EnvVarEntry `json:"vars"`
	// Hash 环境文件的内容哈希（展示前 12 位）：写操作时作为 ifMatch 传回即可获得并发保护
	// —— 环境是整份重写，不带校验会静默抹掉读取之后别人做的改动。
	Hash string `json:"hash,omitempty"`
}

// EnvVarIn create_env 批量建变量时的单条（值可留空，之后用 set_env_var 填）。
type EnvVarIn struct {
	Name   string `json:"name" jsonschema:"变量名（[A-Za-z_][A-Za-z0-9_]*，供 {{name}} 引用）"`
	Value  string `json:"value,omitempty" jsonschema:"变量值"`
	Secret bool   `json:"secret,omitempty" jsonschema:"true = 敏感值（落盘进 .secrets.yml，对外只回掩码）"`
	// Enabled 用指针：不传 / true = 启用，false = 停用（不参与 {{name}} 解析）。
	// 之所以不用 bool：bool 的零值 false 会让「忘了传」变成「建出来就是停用的」——
	// 变量看着在、实际解析不到，是很难查的一类坑。
	Enabled *bool `json:"enabled,omitempty" jsonschema:"不传或 true = 启用；false = 暂时停用（不参与 {{name}} 解析）"`
}

// CreateEnvInput create_env 的入参。
type CreateEnvInput struct {
	Project string     `json:"project" jsonschema:"项目路径、名称或 uid"`
	Name    string     `json:"name" jsonschema:"环境名（文件名即环境名，只允许字母、数字、- 与 _）"`
	Vars    []EnvVarIn `json:"vars,omitempty" jsonschema:"可选：建环境时一并写入的变量"`
}

// SetEnvVarInput set_env_var 的入参（按变量名 upsert）。
type SetEnvVarInput struct {
	Project string `json:"project" jsonschema:"项目路径、名称或 uid"`
	Env     string `json:"env" jsonschema:"所属环境名（用 list_envs 看现有环境）"`
	Name    string `json:"name" jsonschema:"变量名（[A-Za-z_][A-Za-z0-9_]*）"`
	Value   string `json:"value" jsonschema:"变量值；若该变量是敏感值且这里传回掩码，则保持原密钥不变"`
	Secret  bool   `json:"secret,omitempty" jsonschema:"true = 敏感值（值落盘进 .secrets.yml，之后对外只回掩码）"`
	Enabled *bool  `json:"enabled,omitempty" jsonschema:"不传 = 新增则启用、已存在则保持原状；false = 停用该变量"`
	// IfMatch 传 list_envs 返回的 hash（12 位前缀即可）→ 磁盘文件在此期间被外部改动就拒绝写入。
	// 「先 list_envs 再 set_env_var」时带上它，否则期间的其它改动会被整份覆盖。
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"并发保护：传 list_envs 返回的 hash；不传 = 最后写者赢"`
}

// ListEnvs 列出项目全部环境及其变量（敏感值掩码）。
// query 为空即不过滤；非空时按「环境名 / 变量名」子串匹配（忽略大小写与全角空格）。
func (s *Service) ListEnvs(project, query string) ([]EnvEntry, error) {
	a, _, err := s.reg.App(project)
	if err != nil {
		return nil, err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return nil, err
	}
	def := defaultEnvName(envs)
	q := normalize(query)
	out := make([]EnvEntry, 0, len(envs))
	for _, env := range envs {
		vars := make(map[string]EnvVarEntry, len(env.Vars))
		hit := q == "" || strings.Contains(normalize(env.Name), q)
		for _, v := range env.Vars {
			vars[v.Name] = EnvVarEntry{
				Name:    v.Name,
				Value:   maskedValueOf(v),
				Secret:  v.Secret,
				Enabled: v.Enabled,
			}
			if q != "" && strings.Contains(normalize(v.Name), q) {
				hit = true
			}
		}
		if !hit {
			continue
		}
		out = append(out, EnvEntry{
			Name:    env.Name,
			Default: env.Name == def,
			Vars:    vars,
			Hash:    hashOf(a, env.Name),
		})
	}
	return out, nil
}

// CreateEnv 新建环境（可同时写入初始变量）。重名直接报错，不覆盖已有环境。
func (s *Service) CreateEnv(in CreateEnvInput) (EnvEntry, error) {
	name, err := checkEnvName(in.Name)
	if err != nil {
		return EnvEntry{}, err
	}
	a, _, err := s.reg.App(in.Project)
	if err != nil {
		return EnvEntry{}, err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return EnvEntry{}, err
	}
	// 判重必须大小写不敏感：环境名即文件名，Windows 上 "DEV" 与 "dev" 是同一个文件。
	// 原来用 == 判重会让 "DEV" 绕过检查，随后 SaveEnv 截断覆盖 dev.yml，连密钥一起销毁。
	if i := indexOfEnv(envs, name); i >= 0 {
		return EnvEntry{}, fmt.Errorf("环境 %q 已存在（磁盘上实际是 %q；要改它的变量用 set_env_var，要换名字用 rename_env）",
			name, envs[i].Name)
	}
	env := collection.Env{Name: name}
	for _, v := range in.Vars {
		vn := strings.TrimSpace(v.Name)
		if vn == "" {
			continue // 允许批量传草稿、只填名字
		}
		if err := checkVarName(vn); err != nil {
			return EnvEntry{}, err
		}
		env.Vars = append(env.Vars, collection.Var{
			Name:    vn,
			Value:   v.Value,
			Secret:  v.Secret,
			Enabled: v.Enabled == nil || *v.Enabled,
		})
	}
	if err := a.SaveEnv(&env); err != nil {
		return EnvEntry{}, err
	}
	return toEnvEntry(env, ""), nil
}

// RenameEnv 给环境改名：按新名写一份（敏感值一起搬过去），再把旧名移进 .trash。
//
// 不用「直接重命名文件」的原因：环境的两个文件（<env>.yml 与 <env>.secrets.yml）必须同时搬，
// 走 SaveEnv + DeleteEnv 天然覆盖这一步，且旧数据留在 .trash 里可人工找回。
func (s *Service) RenameEnv(project, name, newName, ifMatch string) (EnvEntry, error) {
	oldName, err := checkEnvName(name)
	if err != nil {
		return EnvEntry{}, err
	}
	target, err := checkEnvName(newName)
	if err != nil {
		return EnvEntry{}, err
	}
	a, _, err := s.reg.App(project)
	if err != nil {
		return EnvEntry{}, err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return EnvEntry{}, err
	}
	idx := indexOfEnv(envs, oldName)
	if idx < 0 {
		return EnvEntry{}, fmt.Errorf("找不到环境 %q（用 list_envs 看现有环境）", oldName)
	}
	// 目标名命中了某个已有环境：两种情况都必须拒绝。
	//  ① 命中的是它自己 → 纯改名（完全同名，或仅大小写不同）。仅大小写不同尤其危险：
	//     SaveEnv 会写回同一个文件，紧接着 DeleteEnv 又把它移进 .trash，
	//     结果「改名成功」而环境凭空消失（实测：环境列表变空，只在 .trash 里找得到）。
	//  ② 命中的是别的环境 → 目标名被占用。
	if j := indexOfEnv(envs, target); j >= 0 {
		if j == idx {
			return EnvEntry{}, fmt.Errorf(
				"新环境名 %q 与原环境 %q 在当前文件系统上是同一个文件（文件名不区分大小写），改名不会生效，已拒绝；"+
					"请用一个拼写完全不同的名字", target, envs[idx].Name)
		}
		return EnvEntry{}, fmt.Errorf("环境名 %q 已被 %q 占用，换个名字", target, envs[j].Name)
	}
	// 改名交给 collection 层：并发校验必须落在**旧名**文件上，而在这一层拼
	// 「SaveEnv(新名) + DeleteEnv(旧名)」会让 SaveEnvChecked 去看新文件（不存在 → 哈希为空
	// → 校验被静默跳过），旧名与新名仅大小写不同还会把环境删没。详见 collection.RenameEnv。
	if err := a.RenameEnv(oldName, target, ifMatch); err != nil {
		return EnvEntry{}, err
	}
	moved := envs[idx]
	moved.Name = target
	out := toEnvEntry(moved, "")
	out.Hash = hashOf(a, target)
	return out, nil
}

// DeleteEnv 删除指定环境（文件移入 .trash/，可人工找回）。
//
// 先查存在性：collection 层的删除对「文件不在」是静默成功的（幂等，界面反复点没问题），
// 但对 AI 来说「删掉了」必须为真 —— 不存在时要报错，否则它会以为环境已清理而继续往下做。
func (s *Service) DeleteEnv(project, name string) error {
	clean, err := checkEnvName(name)
	if err != nil {
		return err
	}
	a, _, err := s.reg.App(project)
	if err != nil {
		return err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return err
	}
	if indexOfEnv(envs, clean) < 0 {
		return fmt.Errorf("找不到环境 %q（用 list_envs 看现有环境）", clean)
	}
	return a.DeleteEnv(clean)
}

// SetEnvVar 新增或更新某环境里的一个变量（按名字 upsert）。
//
// 只改这一条：其它变量、顺序、enabled 标记都不动，避免「批量保存」把别人写的内容带歪。
func (s *Service) SetEnvVar(in SetEnvVarInput) (EnvVarEntry, error) {
	name := strings.TrimSpace(in.Name)
	if err := checkVarName(name); err != nil {
		return EnvVarEntry{}, err
	}
	envName, err := checkEnvName(in.Env)
	if err != nil {
		return EnvVarEntry{}, err
	}
	a, _, err := s.reg.App(in.Project)
	if err != nil {
		return EnvVarEntry{}, err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return EnvVarEntry{}, err
	}
	idx := indexOfEnv(envs, envName)
	if idx < 0 {
		return EnvVarEntry{}, fmt.Errorf("找不到环境 %q（用 list_envs 看现有环境；没有就先 create_env）", envName)
	}
	// enabled 先按「传了就用、没传就 true」定初值。
	// 原来这个处理只写在「找到同名变量」的循环体内，于是**新增**变量时 enabled=false
	// 被静默丢弃（实测：传 false 建出来的变量落盘仍是 true），与 schema 承诺矛盾。
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	entry := collection.Var{Name: name, Value: in.Value, Secret: in.Secret, Enabled: enabled}
	kept := false // 本次是否没动密钥
	if !hasVar(envs[idx].Vars, name) && in.Secret && isMaskish(in.Value) {
		// 新建敏感变量的路径没有「旧值」可保留：把掩码当值写进 .secrets.yml 等于存了个假密钥，
		// 而且之后 AI 再也拿不到真值 —— 直接报错，让调用方给真实密钥。
		return EnvVarEntry{}, fmt.Errorf(
			"变量 %s.%s 是新变量，secret=true 时 value 必须是真实密钥（收到的是掩码/占位符 %q），"+
				"否则会把掩码本身存成密钥；若只是想占位，请先传真实值或把 secret 设为 false",
			envName, name, in.Value)
	}
	for i := range envs[idx].Vars {
		if envs[idx].Vars[i].Name != name {
			continue
		}
		old := envs[idx].Vars[i]
		entry = old
		entry.Secret = in.Secret
		switch {
		case old.Secret && isMaskish(in.Value):
			// 关键约定：敏感变量收到掩码/占位 = 「这条密钥我不动」。
			// AI 只能看到掩码，若原样回写就会把 "••••••" 存成真密钥，密钥当场废掉。
			// 用 isMaskish 而不是「== maskedValue」：掩码经过 shell / 编辑器 / 编码转换后
			// 可能变成 •••• 或 **** 甚至 ?????，字节级比较会漏判并把密钥冲掉。
			kept = true
		case old.Secret && in.Value == "":
			// 静默把密钥清空是最坏的一种「帮倒忙」，直接报错让人来决定。
			return EnvVarEntry{}, fmt.Errorf(
				"变量 %s.%s 是敏感值，传空值会清空密钥，已拒绝。"+
					"不改它就把 list_envs 里看到的值原样传回；要轮换就传新密钥；"+
					"确实要清空就把 secret 设为 false 并给一个新值（会把它变成普通变量）", envName, name)
		default:
			entry.Value = in.Value
		}
		// entry = old 已带入原 enabled，所以「不传」= 保持原状（不会被一次无关的值更新意外启用）。
		if in.Enabled != nil {
			entry.Enabled = *in.Enabled
		}
		break
	}
	envs[idx].Vars = replaceVar(envs[idx].Vars, entry)
	// 整份重写之前校验磁盘内容是否仍是读时那份：不校验就会把客户端界面 / 另一个 AI 会话
	// 在这期间新增或修改的变量整份抹掉，而且两边都收到「成功」。
	if err := a.SaveEnvChecked(&envs[idx], in.IfMatch); err != nil {
		return EnvVarEntry{}, err
	}
	return EnvVarEntry{
		Name:       entry.Name,
		Value:      maskedValueOf(entry),
		Secret:     entry.Secret,
		Enabled:    entry.Enabled,
		KeptSecret: kept,
		Hash:       hashOf(a, envName),
	}, nil
}

// isMaskish 判断一个值是否是「掩码/占位」而不是真值。
//
// 命中的字符全是装饰性符号（项目符号、星号、点、问号），现实中不会有密钥长这样；
// 目的是拦住「掩码在传输途中被改写」的情况，避免真密钥被占位符覆盖。
func isMaskish(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// 只认真正的装饰字符。刻意不收 '.' 与 '?'：掩码本身是 U+2022 圆点（共享包
	// varx.MaskedValue），而 '.' / '?' 是现实中可能真实出现的密码字符；收进来会让
	// 「把密钥改成 ...」被当成掩码静默忽略，只在结果里附一句「传回的是掩码」，
	// 调用方无法察觉自己根本没改成。
	for _, r := range s {
		switch r {
		case '•', '●', '·', '*':
		default:
			return false
		}
	}
	return true
}

// DeleteEnvVar 从某环境里删掉一个变量（存在才删；不存在报错，避免「以为删掉了」）。
func (s *Service) DeleteEnvVar(project, envName, varName, ifMatch string) error {
	clean, err := checkEnvName(envName)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(varName)
	if name == "" {
		return errNeedVarName
	}
	a, _, err := s.reg.App(project)
	if err != nil {
		return err
	}
	envs, err := a.ListEnvs()
	if err != nil {
		return err
	}
	idx := indexOfEnv(envs, clean)
	if idx < 0 {
		return fmt.Errorf("找不到环境 %q", clean)
	}
	at := -1
	for i := range envs[idx].Vars {
		if envs[idx].Vars[i].Name == name {
			at = i
			break
		}
	}
	if at < 0 {
		return fmt.Errorf("环境 %q 里没有变量 %q（用 list_envs 看现有变量）", clean, name)
	}
	envs[idx].Vars = append(envs[idx].Vars[:at], envs[idx].Vars[at+1:]...)
	// 同样是「读整份 → 删一条 → 整份写回」，必须带并发校验（理由同 SetEnvVar）。
	return a.SaveEnvChecked(&envs[idx], ifMatch)
}

// ---- 小工具 ----

// hashOf 取环境文件哈希并截到 12 位（与请求详情里的 hash 展示一致，便于直接复制回传）。
func hashOf(a ProjectApp, envName string) string {
	h, err := a.EnvFileHash(envName)
	if err != nil || h == "" {
		return ""
	}
	return h[:min(12, len(h))]
}

// maskedValueOf 敏感变量的对外值：有值回掩码，空值回空串（空串回掩码会谎报「这里有密钥」）。
func maskedValueOf(v collection.Var) string {
	if v.Secret && v.Value != "" {
		return maskedValue
	}
	return v.Value
}

// defaultEnvName 缺省环境名：环境按文件名排序，客户端也取第一个（见前端 collection 存储）。
func defaultEnvName(envs []collection.Env) string {
	if len(envs) == 0 {
		return ""
	}
	return envs[0].Name
}

// indexOfEnv 按名字找环境的下标（-1 = 没有）。
//
// 用 EqualFold 而不是 ==：环境名就是文件名，而目标文件系统（Windows / macOS 默认）
// 不区分大小写。这里若区分，调用方会漏判重名，随后 SaveEnv 会用 os.WriteFile 覆盖同一个
// 文件，把已有环境的变量和 .secrets.yml 里的密钥一起销毁（实测：无报错、无 .trash 备份）。
func indexOfEnv(envs []collection.Env, name string) int {
	want := strings.TrimSpace(name)
	for i := range envs {
		if strings.EqualFold(envs[i].Name, want) {
			return i
		}
	}
	return -1
}

// hasVar 环境里是否已有同名变量。
func hasVar(vars []collection.Var, name string) bool {
	for i := range vars {
		if vars[i].Name == name {
			return true
		}
	}
	return false
}

// replaceVar 覆盖同名变量或追加到末尾（保持既有顺序，避免无意义的文件抖动）。
func replaceVar(vars []collection.Var, v collection.Var) []collection.Var {
	for i := range vars {
		if vars[i].Name == v.Name {
			vars[i] = v
			return vars
		}
	}
	return append(vars, v)
}

// toEnvEntry 把 collection 的环境转成对外结构（敏感值掩码）。
func toEnvEntry(env collection.Env, def string) EnvEntry {
	vars := make(map[string]EnvVarEntry, len(env.Vars))
	for _, v := range env.Vars {
		vars[v.Name] = EnvVarEntry{
			Name:    v.Name,
			Value:   maskedValueOf(v),
			Secret:  v.Secret,
			Enabled: v.Enabled,
		}
	}
	return EnvEntry{Name: env.Name, Default: env.Name == def, Vars: vars}
}

// checkEnvName 校验并返回规范化的环境名：非空、只允许字母数字与 -_（文件名规则）。
func checkEnvName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", errNeedEnvName
	}
	if !sharecoll.ValidEnvName(n) {
		return "", fmt.Errorf("环境名 %q 不合法：只允许字母、数字、- 与 _", n)
	}
	return n, nil
}

// checkVarName 校验变量名：非空、能被 {{name}} 引用、不含路径分隔符。
func checkVarName(name string) error {
	if name == "" {
		return errNeedVarName
	}
	if !varNamePattern.MatchString(name) {
		return fmt.Errorf("变量名 %q 不合法：只允许字母、数字与 _，且不能以数字开头（要能被 {{%s}} 引用）", name, name)
	}
	return nil
}
