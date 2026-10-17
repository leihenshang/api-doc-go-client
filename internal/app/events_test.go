package app

// P2-18：事件总线对慢消费者要「丢最旧」而不是「丢最新」，并累计 dropped。
// 事件语义是状态变更通知：丢新事件会让界面停在过期状态，丢旧事件最坏只少刷一次中间态。

import (
	"testing"
	"time"
)

// drainNames 把通道里现有事件的事件名按序取出（不阻塞）。
func drainNames(ch <-chan map[string]any) []string {
	var out []string
	for {
		select {
		case ev := <-ch:
			name, _ := ev["name"].(string)
			out = append(out, name)
		default:
			return out
		}
	}
}

func TestEmitDropsOldestWhenFull(t *testing.T) {
	a := NewApp()
	ch, unsub := a.SubscribeEvents()
	defer unsub()

	// 通道容量 16：发 20 条，前 4 条应被挤掉，最后留 16 条且第 16 条是最新的。
	const total = 20
	for i := 0; i < total; i++ {
		a.emitAppEvent("ev", i)
	}
	got := drainNames(ch)
	if len(got) != 16 {
		t.Fatalf("积压后应保留 16 条，got %d", len(got))
	}

	// dropped 计数 = 被挤掉的条数
	a.mu.Lock()
	sub := a.eventSubs[1]
	a.mu.Unlock()
	if sub == nil {
		t.Fatal("订阅未登记")
	}
	if sub.dropped.Load() != total-16 {
		t.Fatalf("dropped 应为 %d，got %d", total-16, sub.dropped.Load())
	}
}

// TestEmitKeepsLatestPayload 通道满时保留的最新事件应是最后发出的那条。
func TestEmitKeepsLatestPayload(t *testing.T) {
	a := NewApp()
	ch, unsub := a.SubscribeEvents()
	defer unsub()

	for i := 0; i < 18; i++ { // 容量 16，超 2 条
		a.emitAppEvent("ev", i)
	}
	var newest any
	count := 0
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("通道不应被关闭")
			}
			newest = ev["payload"]
			count++
			continue
		case <-time.After(50 * time.Millisecond):
		}
		break
	}
	if count != 16 {
		t.Fatalf("应收到 16 条，got %d", count)
	}
	if newest != 17 {
		t.Fatalf("最新事件载荷应为 17（最后一条），got %v", newest)
	}
}

// TestUnsubscribeStopsDelivery 退订后不再投递，且不 panic（通道不关闭）。
func TestUnsubscribeStopsDelivery(t *testing.T) {
	a := NewApp()
	ch, unsub := a.SubscribeEvents()
	unsub()
	a.emitAppEvent("ev", 1)
	if got := drainNames(ch); len(got) != 0 {
		t.Fatalf("退订后不应再收到事件，got %v", got)
	}
	// 退订两次也必须安全
	unsub()
}
