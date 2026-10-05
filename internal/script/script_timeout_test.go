package script

import (
	"strings"
	"testing"
	"time"
)

// 死循环脚本必须被中断：否则一次发送会把调用方（IPC / MCP 工具）永久挂死。
// 脚本源码来自请求文件（不可信输入），这不是理论风险。
func TestPreRequestLoopIsInterrupted(t *testing.T) {
	r := New(nil, Scripts{PreRequest: `while (true) {}`})
	start := time.Now()
	out := r.RunPreRequest(nil)
	elapsed := time.Since(start)

	if out.ScriptError == "" {
		t.Fatal("死循环脚本应报错（超时中断）")
	}
	if !strings.Contains(out.ScriptError, "超时") {
		t.Fatalf("错误信息应说明是超时: %s", out.ScriptError)
	}
	if elapsed > execTimeout+3*time.Second {
		t.Fatalf("中断耗时过长: %s", elapsed)
	}
}

// 断言表达式同样是用户提供的 JS：死循环也要被中断，而不是让整次发送无响应。
func TestAssertLoopIsInterrupted(t *testing.T) {
	r := New(nil, Scripts{})
	out := r.RunPostResponse(&Response{Status: 200, Headers: map[string]string{}, Body: nil}, []Assert{
		{Name: "loop", Expr: "(function(){ while(true){} })()"},
	})
	if len(out.Asserts) != 1 {
		t.Fatalf("断言条数: %d", len(out.Asserts))
	}
	if out.Asserts[0].Passed {
		t.Fatal("死循环断言不应通过")
	}
	if !strings.Contains(out.Asserts[0].Error, "超时") {
		t.Fatalf("断言错误应说明超时: %s", out.Asserts[0].Error)
	}
}

// 正常脚本与断言不受超时机制影响。
func TestNormalScriptStillWorks(t *testing.T) {
	r := New(map[string]string{"host": "http://x"}, Scripts{PreRequest: `bru.setVar('a', '1')`})
	if out := r.RunPreRequest(nil); out.ScriptError != "" {
		t.Fatalf("正常脚本报错: %s", out.ScriptError)
	}
	out := r.RunPostResponse(&Response{Status: 201, Headers: map[string]string{}, Body: nil}, []Assert{
		{Name: "status", Expr: "res.status === 201"},
	})
	if len(out.Asserts) != 1 || !out.Asserts[0].Passed {
		t.Fatalf("正常断言未通过: %+v", out.Asserts)
	}
}
