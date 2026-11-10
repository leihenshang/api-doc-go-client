package collection

// CloneDoc 在同一分组内复制一篇文档，显示名加「副本N」，正文 / 图标 / 排序一并带上。
//
// folder 为空时要传 "."：CreateDocWithOpts 把空串解释为默认目录 docs/，
// 会让「根目录下的文档」复制到 docs/ 里，看起来像是挪走了。
func (c *Collection) CloneDoc(uid string) (*DocEntry, error) {
	src, err := c.ReadDoc(uid)
	if err != nil {
		return nil, err
	}
	folder := folderOfPath(src.Path)
	name, err := c.nextCloneName(folder, src.Name)
	if err != nil {
		return nil, err
	}
	target := folder
	if target == "" {
		target = "."
	}
	d, err := c.CreateDocWithOpts(CreateDocOpts{Name: name, Folder: target, OnConflict: DocConflictError})
	if err != nil {
		return nil, err
	}
	d.Content = src.Content
	d.Icon = src.Icon
	// seq 用 CreateDocWithOpts 新分配的：沿用源 seq 会和源挤在同一位置，排序不稳定。
	if err := c.SaveDoc(d); err != nil {
		return nil, err
	}
	return d, nil
}
