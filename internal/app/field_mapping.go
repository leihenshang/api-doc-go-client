package app

import (
	"api-doc-go-client/internal/collection"
)

// ListFieldMappings 当前集合的字段字典（项目级，随集合同步）。
func (a *App) ListFieldMappings() ([]collection.FieldMapping, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.ListFieldMappings()
}

// SaveFieldMapping 新增或更新一条字段字典（uid 为空时新建），返回落盘后的条目。
func (a *App) SaveFieldMapping(fm collection.FieldMapping) (*collection.FieldMapping, error) {
	c, err := a.requireCollection()
	if err != nil {
		return nil, err
	}
	return c.UpsertFieldMapping(fm)
}

// DeleteFieldMapping 删除一条字段字典。
func (a *App) DeleteFieldMapping(uid string) error {
	c, err := a.requireCollection()
	if err != nil {
		return err
	}
	return c.DeleteFieldMapping(uid)
}
