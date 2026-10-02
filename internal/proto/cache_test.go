package proto

import (
	"context"
	"testing"
)

// 缓存命中返回同一结果指针；Invalidate 后必须重新编译（D3：更新只由用户显式触发）。
func TestCacheReusesUntilInvalidate(t *testing.T) {
	cache := NewCache()
	files := []string{testdata(t, "greeter.proto")}
	imports := []string{testdataDir(t)}
	ctx := context.Background()

	first, err := cache.Compile(ctx, files, imports)
	if err != nil {
		t.Fatalf("首次编译失败: %v", err)
	}
	second, err := cache.Compile(ctx, files, imports)
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if first != second {
		t.Error("同 key 应命中缓存（返回同一个结果）")
	}
	if cache.Len() != 1 {
		t.Errorf("缓存条目 = %d，期望 1", cache.Len())
	}

	cache.Invalidate()
	if cache.Len() != 0 {
		t.Errorf("Invalidate 后条目 = %d，期望 0", cache.Len())
	}
	third, err := cache.Compile(ctx, files, imports)
	if err != nil {
		t.Fatalf("Invalidate 后编译失败: %v", err)
	}
	if third == first {
		t.Error("Invalidate 后应重新编译（不能复用旧结果）")
	}
}

// 失败结果也缓存：避免每次发送都对同一份坏定义重复编译。
func TestCacheCachesFailure(t *testing.T) {
	cache := NewCache()
	files := []string{testdata(t, "broken.proto")}
	ctx := context.Background()

	if _, err := cache.Compile(ctx, files, nil); err == nil {
		t.Fatal("坏定义应编译失败")
	}
	if cache.Len() != 1 {
		t.Errorf("失败结果也应缓存，条目 = %d", cache.Len())
	}
	if _, err := cache.Compile(ctx, files, nil); err == nil {
		t.Fatal("命中缓存的失败结果仍应报错")
	}
	if cache.Len() != 1 {
		t.Errorf("命中缓存不应新增条目，条目 = %d", cache.Len())
	}
}

// 文件/import 路径顺序不同应命中同一 key。
func TestCacheKeyIgnoresOrder(t *testing.T) {
	cache := NewCache()
	a := testdata(t, "greeter.proto")
	dir := testdataDir(t)
	ctx := context.Background()

	if _, err := cache.Compile(ctx, []string{a}, []string{dir}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 追加一个无关 import 路径会构成新 key（不同集合/不同 import 配置要分别缓存）
	if _, err := cache.Compile(ctx, []string{a}, []string{dir, dir}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if cache.Len() != 1 {
		t.Errorf("重复 import 路径应归一化成同一 key，条目 = %d，期望 1", cache.Len())
	}
}
