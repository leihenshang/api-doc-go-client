package mcp

// update_requests：批量局部更新（1–10 条）。
//
// 为什么需要它：迁移/统一补说明时，AI 只能逐个 update_request —— 每条一次往返、一次授权弹窗。
// 批量版把 N 次调用压成 1 次；失败条目逐条回报（与 get_request_detail 的 uids 批量同口径），
// 单条失败不影响其它条目。
//
// 为什么 items 不覆盖 update_request 的全部字段：respDoc / auth / settings 体积大、批量场景极少用，
// 留在 items 里会让 inputSchema 膨胀（实测曾到 12.9KB）；需要时逐条调 update_request 即可。

import (
	"errors"
	"fmt"
	"strings"
)

// maxImportTextBytes import_collection 单次正文上限（整包一次性解析，防止超大输入拖垮进程）。
const maxImportTextBytes = 10 << 20

// maxBatchUpdate 与 delete_requests / get_request_detail 的批量上限保持一致。
const maxBatchUpdate = 10

// UpdateRequestItem 批量更新里的一条：uid + 要改的字段（与 update_request 同名同义）。
type UpdateRequestItem struct {
	UID     string `json:"uid,omitempty" jsonschema:"要更新的接口 uid；与 path 二选一"`
	Path    string `json:"path,omitempty" jsonschema:"请求文件路径（相对集合根，如 api/users.yml 或 api/users）；与 uid 二选一"`
	IfMatch string `json:"ifMatch,omitempty" jsonschema:"该接口当前 hash（search_requests / get_request_detail 返回）；不传 = 最后写者赢"`
	Section string `json:"section,omitempty" jsonschema:"节点守卫（同 update_request）：request / response / docs；省略 = 不限制"`

	Name   *string `json:"name,omitempty"`
	Method *string `json:"method,omitempty"`
	URL    *string `json:"url,omitempty"`

	Params  []KVInput   `json:"params,omitempty" jsonschema:"参数表整表覆盖；与 patch.params 互斥"`
	Patch   *PatchInput `json:"patch,omitempty" jsonschema:"行级合并：{params, headers, bodyFields}；与对应的整表字段互斥"`
	Headers []KVInput   `json:"headers,omitempty" jsonschema:"请求头整表覆盖；与 patch.headers 互斥"`

	Body       *BodyInput        `json:"body,omitempty" jsonschema:"请求体局部更新：不传的子项保留原值"`
	BodyFields *[]BodyFieldInput `json:"bodyFields,omitempty" jsonschema:"字段表整表覆盖；与 patch.bodyFields 互斥"`

	Summary  *string     `json:"summary,omitempty"`
	Docs     *string     `json:"docs,omitempty" jsonschema:"接口说明整篇覆盖；与 replaces 互斥"`
	Replaces []ReplaceOp `json:"replaces,omitempty" jsonschema:"接口说明局部替换；任一 old 找不到则该条失败（不影响其他条）"`
	Folder   *string     `json:"folder,omitempty" jsonschema:"移动到别的模块（相对集合根；空串 = 项目根）"`
}

// UpdateRequestsInput update_requests 的入参。
type UpdateRequestsInput struct {
	Project string              `json:"project,omitempty" jsonschema:"项目路径/名称/uid；省略 = 会话默认工作目录"`
	Items   []UpdateRequestItem `json:"items" jsonschema:"1–10 条；每条给 uid + 要改的字段（语义同 update_request）"`
	DryRun  bool                `json:"dryRun,omitempty" jsonschema:"true = 只校验并回报将改动哪些条目，不写盘"`
}

// BatchUpdateFailure 单条失败的明细。
type BatchUpdateFailure struct {
	Index int    `json:"index"` // 对应 items 里的下标（0 基）：同批重复 uid 时靠它定位
	UID   string `json:"uid"`
	Path  string `json:"path,omitempty"` // 按 path 定位时回填，便于调用方对上号
	Code  string `json:"code"`           // 稳定错误码（NOT_FOUND / CONFLICT / INVALID …），便于调用方分流
	Error string `json:"error"`          // 去掉 `[CODE] ` 前缀后的说明
}

// UpdateRequests 逐条应用局部更新；单条失败不影响其它条目。
// 返回成功条目的 uid 列表与失败明细（调用方按 uid 分组展示）。
func (s *Service) UpdateRequests(in UpdateRequestsInput) ([]string, []BatchUpdateFailure, error) {
	if len(in.Items) == 0 {
		return nil, nil, withInvalid(errors.New("items 不能为空：一次传 1–10 条（每条给 uid + 要改的字段）"))
	}
	if len(in.Items) > maxBatchUpdate {
		return nil, nil, withLimit(fmt.Errorf("items 一次最多 %d 条（本次 %d 条）：请分批调用", maxBatchUpdate, len(in.Items)))
	}
	// 先确认项目可写：项目级错误应整体失败，而不是逐条报同一个错。
	a, _, err := s.resolveWrite(in.Project)
	if err != nil {
		return nil, nil, err
	}
	// 批量开始刷新一次索引，逐条不再重复 resolve / 刷新。
	if _, rerr := a.ReloadCollection(); rerr != nil {
		return nil, nil, coded(CodeIO, rerr)
	}
	updated := make([]string, 0, len(in.Items))
	failed := make([]BatchUpdateFailure, 0)
	for i, it := range in.Items {
		uid := strings.TrimSpace(it.UID)
		p := strings.TrimSpace(it.Path)
		if uid == "" && p == "" {
			failed = append(failed, BatchUpdateFailure{Index: i, Code: CodeInvalid, Error: "缺少 uid 或 path（二选一）"})
			continue
		}
		r, err := s.updateRequestWithApp(a, itemToInput(in.Project, uid, p, it, in.DryRun))
		if err != nil {
			code := CodeOf(err)
			failed = append(failed, BatchUpdateFailure{
				Index: i,
				UID:   uid,
				Path:  p,
				Code:  code,
				Error: strings.TrimPrefix(err.Error(), "["+code+"] "),
			})
			continue
		}
		updated = append(updated, r.UID) // 按 path 定位时回填解析后的 uid
	}
	// 末尾再刷一次：让后续读（list/get）看到本批结果；dryRun 没有写盘，不需要。
	if !in.DryRun {
		_, _ = a.ReloadCollection()
	}
	return updated, failed, nil
}

// itemToInput 把批量子项摊平成单条 UpdateRequestInput（字段一一对应）。
func itemToInput(project, uid, reqPath string, it UpdateRequestItem, dryRun bool) UpdateRequestInput {
	return UpdateRequestInput{
		Project:    project,
		UID:        uid,
		Path:       reqPath,
		IfMatch:    it.IfMatch,
		Section:    it.Section,
		Name:       it.Name,
		Method:     it.Method,
		URL:        it.URL,
		Params:     it.Params,
		Patch:      it.Patch,
		Headers:    it.Headers,
		Body:       it.Body,
		BodyFields: it.BodyFields,
		Summary:    it.Summary,
		Docs:       it.Docs,
		Replaces:   it.Replaces,
		Folder:     it.Folder,
		DryRun:     dryRun,
	}
}
