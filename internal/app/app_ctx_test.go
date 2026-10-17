package app

// P2-9：Wails 运行时上下文存结构体是既定形态，但读写必须在同一把锁下。
// 这里一边写一边读，配合 -race 复现；并验证「未就绪」时各绑定方法安全返回而不是 panic。

import (
	"context"
	"runtime"
	"sync"
	"testing"
)

func TestAppCtxConcurrentSetGet(t *testing.T) {
	a := NewApp()
	var wg sync.WaitGroup

	// 读写必须真正交错：读写都放进 goroutine，并且让写侧跑够多次，
	// 否则主 goroutine 先把 100 次写完，读侧根本没机会与之重叠（变异验证会漏）。
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = a.appCtx()
					runtime.Gosched()
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for i := 0; i < 2; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 2000; j++ {
				a.setAppCtx(context.Background())
				runtime.Gosched()
			}
		}()
	}
	writers.Wait()
	close(stop)
	wg.Wait()

	if a.appCtx() == nil {
		t.Fatal("setAppCtx 之后 appCtx 不应为 nil")
	}
}

// TestNilCtxMethodsAreSafe 非桌面形态（devserver）下 ctx 为 nil，绑定方法必须安全返回。
func TestNilCtxMethodsAreSafe(t *testing.T) {
	a := NewApp()
	if ctx := a.appCtx(); ctx != nil {
		t.Fatalf("零值 App 的 appCtx 应为 nil，got %v", ctx)
	}
	if _, err := a.PickDirectory(); err == nil {
		t.Fatal("未就绪时 PickDirectory 应返回错误")
	}
	if _, err := a.PickFile(); err == nil {
		t.Fatal("未就绪时 PickFile 应返回错误")
	}
	// 窗口控制类：静默忽略，不能 panic
	a.WindowMinimise()
	a.WindowToggleMaximise()
	if a.WindowIsMaximised() {
		t.Fatal("未就绪时 IsMaximised 应为 false")
	}
}
