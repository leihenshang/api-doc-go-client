package testrun

// 集合运行报告：text / JSON / JUnit 三种输出（RM-02）。
// JSON 给机器读；JUnit 给 CI 的测试报告解析；text 给人看。

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api-doc-go-client/internal/script"
)

// RunCase 一条请求的执行结果。
type RunCase struct {
	Name        string                `json:"name"`
	Path        string                `json:"path"`
	UID         string                `json:"uid"`
	Method      string                `json:"method"`
	URL         string                `json:"url"`
	Status      int                   `json:"status"`
	Proto       string                `json:"proto,omitempty"`
	DurationMS  int64                 `json:"duration_ms"`
	Passed      bool                  `json:"passed"`
	Error       string                `json:"error,omitempty"`
	ScriptError string                `json:"script_error,omitempty"`
	Asserts     []script.AssertResult `json:"asserts,omitempty"`
	// Vars 只用于同一轮内的变量链式传递，绝不进报告（可能含 secret/token）。
	Vars map[string]string `json:"-"`
}

// Report 一次集合运行的总报告。
type Report struct {
	Collection string    `json:"collection"`
	Env        string    `json:"env"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	Total      int       `json:"total"`
	Passed     int       `json:"passed"`
	Failed     int       `json:"failed"`
	Cases      []RunCase `json:"cases"`
}

// finish 统计总数/通过/失败与总耗时。
func (r *Report) finish() {
	r.Total = len(r.Cases)
	r.Passed, r.Failed = 0, 0
	for _, c := range r.Cases {
		if c.Passed {
			r.Passed++
		} else {
			r.Failed++
		}
	}
	r.DurationMS = time.Since(r.StartedAt).Milliseconds()
}

// JSON 输出缩进 JSON。
func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Text 输出人类可读的逐条结果与汇总。
func (r *Report) Text() string {
	var b strings.Builder
	env := ""
	if r.Env != "" {
		env = " (env: " + r.Env + ")"
	}
	fmt.Fprintf(&b, "RUN  %s%s\n", r.Collection, env)
	for _, c := range r.Cases {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "%s  %-6s %-4d %6dms  %s\n", status, c.Method, c.Status, c.DurationMS, c.Name)
		if c.Error != "" {
			fmt.Fprintf(&b, "      error: %s\n", c.Error)
		}
		if c.ScriptError != "" {
			fmt.Fprintf(&b, "      script: %s\n", c.ScriptError)
		}
		for _, a := range c.Asserts {
			if a.Passed {
				continue
			}
			detail := a.Error
			if detail == "" {
				detail = "断言为 false"
			}
			fmt.Fprintf(&b, "      assert %q: %s\n", a.Name, detail)
		}
	}
	fmt.Fprintf(&b, "\n%d passed, %d failed, %d total (%dms)\n", r.Passed, r.Failed, r.Total, r.DurationMS)
	return b.String()
}

// JUnit 输出 JUnit XML（CI 测试报告通用格式）。
func (r *Report) JUnit() ([]byte, error) {
	suite := junitSuite{
		Name:     r.Collection,
		Tests:    r.Total,
		Failures: r.Failed,
		Time:     seconds(r.DurationMS),
		Cases:    make([]junitCase, 0, len(r.Cases)),
	}
	for _, c := range r.Cases {
		jc := junitCase{
			Classname: c.Path,
			Name:      c.Name,
			Time:      seconds(c.DurationMS),
		}
		if !c.Passed {
			jc.Failure = &junitFailure{
				Message: failureMessage(c),
				Body:    failureBody(c),
			}
		}
		suite.Cases = append(suite.Cases, jc)
	}
	root := junitSuites{
		Tests:    r.Total,
		Failures: r.Failed,
		Time:     seconds(r.DurationMS),
		Suites:   []junitSuite{suite},
	}
	data, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

// failureMessage 失败用例的一行摘要。
func failureMessage(c RunCase) string {
	switch {
	case c.Error != "":
		return c.Error
	case c.ScriptError != "":
		return c.ScriptError
	default:
		for _, a := range c.Asserts {
			if !a.Passed {
				return "断言失败：" + a.Name
			}
		}
		return "失败"
	}
}

// failureBody 失败用例的明细（断言名 + 表达式 + 错误）。
func failureBody(c RunCase) string {
	var b strings.Builder
	if c.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", c.Error)
	}
	if c.ScriptError != "" {
		fmt.Fprintf(&b, "script: %s\n", c.ScriptError)
	}
	for _, a := range c.Asserts {
		if a.Passed {
			continue
		}
		fmt.Fprintf(&b, "assert %q: %s", a.Name, a.Expr)
		if a.Error != "" {
			fmt.Fprintf(&b, " (%s)", a.Error)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// seconds 毫秒 → JUnit 的秒字符串。
func seconds(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64)
}

// JUnit XML 结构。
type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Time     string      `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}
