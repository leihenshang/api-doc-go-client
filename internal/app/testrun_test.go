package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RM-02：桌面端 RunCollection 与 ExportRunReport 走同一份 testrun 报告。
func TestRunCollectionAndExportJUnit(t *testing.T) {
	core, dir := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c, err := core.requireCollection()
	if err != nil {
		t.Fatalf("requireCollection: %v", err)
	}
	r, err := c.CreateRequest("", "smoke", "GET")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	r.URL = srv.URL + "/health"
	if err := c.SaveRequest(r); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	rep, err := core.RunCollection(RunCollectionOptions{})
	if err != nil {
		t.Fatalf("RunCollection: %v", err)
	}
	if rep.Total != 1 || rep.Passed != 1 || rep.Failed != 0 {
		t.Fatalf("报告异常: %+v", rep)
	}
	if len(rep.Cases) != 1 || !rep.Cases[0].Passed || rep.Cases[0].Status != 200 {
		t.Fatalf("用例结果异常: %+v", rep.Cases)
	}

	path, err := core.ExportRunReport("junit", "")
	if err != nil {
		t.Fatalf("ExportRunReport: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("headless 导出应落在集合目录，got %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读报告: %v", err)
	}
	if !strings.Contains(string(data), "<testsuites") || !strings.Contains(string(data), `tests="1"`) {
		t.Fatalf("JUnit 内容异常:\n%s", data)
	}
}
