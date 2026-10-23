// clientctl 客户端内核的命令行入口（P2 内核验证）：
//
//	clientctl init <dir>                    初始化集合 + 示例请求/环境
//	clientctl list <dir>                    打印集合树
//	clientctl send <dir> <uid> [-env name]  渲染并真实发送一条请求
//	clientctl run  <dir> [flags]             按集合树顺序运行并输出报告（CI 用）
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/runner"
	"api-doc-go-client/internal/testrun"
	"api-doc-go-client/internal/varx"
)

// sendOptions 与桌面端一致：全局设置 + 持久化 Cookie 罐。
func sendOptions() runner.Options {
	s, err := config.Load()
	if err != nil {
		s = config.Default()
	}
	var jar http.CookieJar
	if j, err := cookiejar.New(s.PersistCookies); err == nil {
		jar = j
	}
	return runner.NewOptions(runner.OptionsInput{
		InsecureSSL:     s.InsecureSSL,
		TimeoutSec:      s.TimeoutSec,
		FollowRedirects: s.FollowRedirects,
		MaxRedirects:    s.MaxRedirects,
		Jar:             jar,
		ProxyURL:        s.ProxyURL,
	})
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "init":
		if len(os.Args) < 3 {
			usage()
		}
		initCmd(os.Args[2])
	case "list":
		if len(os.Args) < 3 {
			usage()
		}
		listCmd(os.Args[2])
	case "send":
		fs := flag.NewFlagSet("send", flag.ExitOnError)
		env := fs.String("env", "dev", "使用的环境名")
		_ = fs.Parse(os.Args[3:])
		if len(fs.Args()) < 2 {
			usage()
		}
		sendCmd(fs.Args()[0], fs.Args()[1], *env)
	case "run":
		runArgs(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法:
  clientctl init  <dir>                     初始化集合并写入示例
  clientctl list  <dir>                     打印集合树
  clientctl send  <dir> <uid> [-env name]   渲染并发送请求
  clientctl run   <dir> [flags]             按树顺序运行集合（CI）

run flags:
  -env name          环境名（空 = 仅内置变量）
  -filter substr     按请求名称/路径过滤
  -bail              首个失败即停止
  -timeout seconds   单请求超时（0 = 全局/请求设置）
  -reporter format   text | json | junit（默认 text）
  -output file       报告输出文件（空 = stdout）`)
	os.Exit(1)
}

// runFlags clientctl run 的参数。
type runFlags struct {
	dir, env, filter string
	bail             bool
	timeout          int
	reporter, output string
}

// runArgs 解析 run 子命令：同时支持 `run <dir> --env dev` 与 `run --env dev <dir>`。
func runArgs(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	env := fs.String("env", "", "环境名（空 = 仅内置变量）")
	filter := fs.String("filter", "", "按请求名称/路径过滤")
	bail := fs.Bool("bail", false, "首个失败即停止")
	timeout := fs.Int("timeout", 0, "单请求超时（秒）")
	reporter := fs.String("reporter", "text", "text | json | junit")
	output := fs.String("output", "", "报告输出文件")
	dir, rest := splitDirArg(args)
	_ = fs.Parse(rest)
	if dir == "" && len(fs.Args()) > 0 {
		dir = fs.Args()[0]
	}
	if dir == "" {
		usage()
	}
	runCmd(runFlags{dir: dir, env: *env, filter: *filter, bail: *bail,
		timeout: *timeout, reporter: *reporter, output: *output})
}

// splitDirArg 把第一个非 flag 参数当作目录，其余交给 flag 解析。
func splitDirArg(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// runCmd 打开集合并执行，输出报告；有失败时退出码 1。
func runCmd(f runFlags) {
	c, err := collection.Open(f.dir)
	if err != nil {
		fatal(err)
	}
	rep, runErr := testrun.Run(context.Background(), c, testrun.Options{
		EnvName: f.env, Filter: f.filter, Bail: f.bail,
		Timeout: time.Duration(f.timeout) * time.Second,
		Runner:  sendOptions(),
	})
	if err := c.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "关闭集合:", err)
	}
	if runErr != nil {
		fatal(runErr)
	}
	data, err := renderReport(rep, f.reporter)
	if err != nil {
		fatal(err)
	}
	if f.output != "" {
		if err := os.WriteFile(f.output, data, 0o644); err != nil {
			fatal(err)
		}
	} else {
		_, _ = os.Stdout.Write(data)
	}
	if rep.Failed > 0 {
		os.Exit(1)
	}
}

// renderReport 按格式渲染报告。
func renderReport(rep *testrun.Report, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return []byte(rep.Text()), nil
	case "json":
		return rep.JSON()
	case "junit":
		return rep.JUnit()
	default:
		return nil, fmt.Errorf("未知报告格式 %q（text|json|junit）", format)
	}
}

func initCmd(dir string) {
	c, err := collection.Open(dir)
	if err != nil {
		fatal(err)
	}
	// dev 环境补充示例变量
	envs, err := c.ListEnvs()
	if err != nil {
		fatal(err)
	}
	for i := range envs {
		if envs[i].Name == "dev" {
			envs[i].Vars = append(envs[i].Vars, collection.Var{Name: "name", Value: "world", Enabled: true})
			if err := c.SaveEnv(envs[i]); err != nil {
				fatal(err)
			}
		}
	}
	if err := c.CreateFolder("", "示例分组"); err != nil {
		fatal(err)
	}
	r, err := c.CreateRequest("示例分组", "示例-GET", "GET")
	if err != nil {
		fatal(err)
	}
	r.URL = "{{host}}/echo?name={{name}}"
	if err := c.SaveRequest(r); err != nil {
		fatal(err)
	}
	fmt.Printf("集合已初始化: %s\n示例请求 uid: %s\n", dir, r.UID)
}

func listCmd(dir string) {
	c, err := collection.Open(dir)
	if err != nil {
		fatal(err)
	}
	tree, err := c.Tree()
	if err != nil {
		fatal(err)
	}
	var walk func(nodes []*collection.Node, depth int)
	walk = func(nodes []*collection.Node, depth int) {
		for _, n := range nodes {
			fmt.Printf("%s%-8s %-14s %s\n", strings.Repeat("  ", depth), "["+n.Type+"]", n.Method, n.Name)
			if n.Children != nil {
				walk(n.Children, depth+1)
			}
		}
	}
	walk(tree, 0)
	envs, _ := c.ListEnvs()
	for _, e := range envs {
		fmt.Printf("[env] %s (%d 个变量)\n", e.Name, len(e.Vars))
	}
}

func sendCmd(dir, uid, envName string) {
	c, err := collection.Open(dir)
	if err != nil {
		fatal(err)
	}
	r, err := c.ReadRequest(uid)
	if err != nil {
		fatal(err)
	}
	envs, err := c.ListEnvs()
	if err != nil {
		fatal(err)
	}
	vars := map[string]string{}
	for _, e := range envs {
		if e.Name != envName {
			continue
		}
		for _, v := range e.Vars {
			if v.Enabled && v.Name != "" {
				vars[v.Name] = v.Value
			}
		}
	}
	for k, v := range varx.Builtins() {
		vars[k] = v
	}
	res, err := runner.Send(context.Background(), *r, vars, sendOptions())
	if err != nil {
		fatal(err)
	}
	fmt.Printf("URL:    %s\nStatus: %d %s (%dms, %d bytes)\n", res.URL, res.Status, res.Proto, res.TimeMS, res.Size)
	if pretty, err := json.MarshalIndent(json.RawMessage(res.Body), "", "  "); err == nil {
		fmt.Println(string(pretty))
	} else {
		fmt.Println(res.Body)
	}
}
