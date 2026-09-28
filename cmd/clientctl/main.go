// clientctl 客户端内核的命令行入口（P2 内核验证）：
//
//	clientctl init <dir>                    初始化集合 + 示例请求/环境
//	clientctl list <dir>                    打印集合树
//	clientctl send <dir> <uid> [-env name]  渲染并真实发送一条请求
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"api-doc-go-client/internal/collection"
	"api-doc-go-client/internal/config"
	"api-doc-go-client/internal/cookiejar"
	"api-doc-go-client/internal/runner"
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
	return runner.NewOptions(s.InsecureSSL, s.TimeoutSec, s.FollowRedirects, s.MaxRedirects, jar)
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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法:
  clientctl init  <dir>                     初始化集合并写入示例
  clientctl list  <dir>                     打印集合树
  clientctl send  <dir> <uid> [-env name]   渲染并发送请求`)
	os.Exit(1)
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
	res, err := runner.Send(*r, vars, sendOptions())
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
