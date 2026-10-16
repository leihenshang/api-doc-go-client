package collection

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// BenchmarkScanLarge 是 C3「大集合扫描」的评估基线：物化 N 个请求后测全量 scan() 与
// RebuildIndex() 的单次成本。日常增量路径由 fsnotify（D1/D2）驱动、不做全量扫描，
// 这里度量的是首次打开/手动重建的兜底代价。
//
//	go test -bench BenchmarkScanLarge -benchtime=1x ./internal/collection/
func BenchmarkScanLarge(b *testing.B) {
	const n = 1000
	c := newBenchCollection(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.scan(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRebuildIndexLarge 同上：全量索引重建（scan + upsert）。
func BenchmarkRebuildIndexLarge(b *testing.B) {
	const n = 1000
	c := newBenchCollection(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.RebuildIndex(); err != nil {
			b.Fatal(err)
		}
	}
}

// newBenchCollection 物化一个含 n 个请求的临时集合（不进版本库，t.TempDir 自动清理）。
func newBenchCollection(b *testing.B, n int) *Collection {
	b.Helper()
	dir := b.TempDir()
	c, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		r := &Request{UID: uuid.NewString(), Name: fmt.Sprintf("api-%04d", i), Method: "GET", URL: "https://example.com/x"}
		r.Path = fmt.Sprintf("bench/f%02d/api-%04d.yml", i%16, i)
		if err := c.SaveRequest(r); err != nil {
			b.Fatal(err)
		}
	}
	return c
}
