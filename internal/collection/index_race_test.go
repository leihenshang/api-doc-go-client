package collection

// P1-10：索引句柄（c.idx）的并发访问。ensureIndex 会在任意调用 goroutine 上惰性打开，
// Close / SetIndexKey / CloseIndex 会在另一条 goroutine 上关闭并置 nil —— 裸字段就是数据竞争。
// 这里直接把「一边用、一边关」跑起来，配合 -race 复现；断言只要求不 panic、不返回脏数据。

import (
	"sync"
	"testing"
)

func TestIndexHandleConcurrentUseAndClose(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := c.CreateRequest("", "并发请求", "GET"); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 读侧：搜索 / 读 hash / 只读 dirty 集
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = c.Search("并发", 10)
				_ = c.NodeHash("whatever")
				_, _ = c.DirtyNodesFast()
			}
		}()
	}

	// 写侧：关闭 / 重开索引句柄（与 App 里切换集合、SetIndexKey 的时序等价）
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c.CloseIndex()
				_ = c.RebuildIndex()
			}
		}()
	}

	// 跑一小会儿：-race 下若有裸字段读写会直接报 DATA RACE
	for i := 0; i < 50; i++ {
		_, _ = c.Search("", 5)
	}
	close(stop)
	wg.Wait()

	// 关完之后仍要能重开（ensureIndex 可重复调用），不能留下「永久关闭」状态
	if _, err := c.ListAllRequests(); err != nil {
		t.Fatalf("关闭/重开后应仍可使用: %v", err)
	}
}

// TestSetIndexKeyBeforePublish 索引键只在集合还没登记时才允许改（App 侧约束的底层保证）：
// 改键会关闭旧句柄，因此必须在任何人拿到引用之前完成。这里验证改键后能按新键重建。
func TestSetIndexKeyBeforePublish(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := c.CreateRequest("", "改键前", "GET"); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	c.SetIndexKey(c.UID + "--copy")
	if err := c.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	reqs, err := c.ListAllRequests()
	if err != nil || len(reqs) != 1 {
		t.Fatalf("改键重建后应仍能列出请求: %v (%d)", err, len(reqs))
	}
	if nodes, err := c.Search("改键前", 10); err != nil || len(nodes) == 0 {
		t.Fatalf("改键后索引应可用: %v (%d)", err, len(nodes))
	}
}
