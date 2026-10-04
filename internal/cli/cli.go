package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jicheng1014/okuptime-agent/internal/api"
	"github.com/jicheng1014/okuptime-agent/internal/config"
	"github.com/jicheng1014/okuptime-agent/internal/update"
)

type monitor struct {
	ID                 int64   `json:"id"`
	ProjectID          int64   `json:"project_id"`
	URL                string  `json:"url"`
	Description        string  `json:"description"`
	MonitorInterval    int     `json:"monitor_interval"`
	CheckStatus        string  `json:"check_status"`
	UptimeStatus       string  `json:"uptime_status"`
	CheckedAt          *string `json:"checked_at"`
	LastStatusCode     *int    `json:"last_status_code"`
	LastResponseTimeMS *int    `json:"last_response_time_ms"`
}

type project struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	return RunWithBuildInfo(args, in, out, errOut, "dev", "")
}

func RunWithBuildInfo(args []string, in io.Reader, out, errOut io.Writer, version, publicKey string) int {
	jsonOutput := hasJSONFlag(args)
	if err := runWithBuildInfo(args, in, out, errOut, version, publicKey); err != nil {
		if jsonOutput {
			var apiErr *api.Error
			if !errors.As(err, &apiErr) {
				apiErr = &api.Error{Code: "cli_error", Message: err.Error()}
			}
			_ = json.NewEncoder(out).Encode(map[string]any{"error": apiErr})
		} else {
			fmt.Fprintln(errOut, "错误:", err)
		}
		return 1
	}
	clean := withoutJSONFlag(args)
	if len(clean) > 0 && (clean[0] == "project" || clean[0] == "monitor") {
		update.Notify(version, errOut)
	}
	return 0
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	return runWithBuildInfo(args, in, out, errOut, "dev", "")
}

func runWithBuildInfo(args []string, in io.Reader, out, errOut io.Writer, version, publicKey string) error {
	jsonOutput := hasJSONFlag(args)
	args = withoutJSONFlag(args)
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, usage)
		return nil
	}

	if len(args) == 1 && args[0] == "version" {
		if jsonOutput {
			return json.NewEncoder(out).Encode(map[string]any{"data": map[string]string{"version": version}})
		}
		fmt.Fprintln(out, "okuptime", version)
		return nil
	}
	if args[0] == "update" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--check") {
			return usageError("用法: okuptime update [--check] [--json]")
		}
		result, err := update.Check(version, 20*time.Second)
		if err != nil {
			return err
		}
		if len(args) == 1 && result.Available {
			stagedPath, err := update.Apply(version, publicKey, result.Release)
			if err != nil {
				return err
			}
			result.Staged = stagedPath != ""
			result.Updated = !result.Staged
			result.StagedPath = stagedPath
			if result.Staged {
				result.FinalizeCommand = update.FinalizeCommand(stagedPath, result.Release.SHA256)
			}
		}
		if jsonOutput {
			return json.NewEncoder(out).Encode(map[string]any{"data": result})
		}
		switch {
		case result.Staged:
			fmt.Fprintf(out, "已下载并验证 %s，尚未替换当前 Windows CLI；本命令退出后在 PowerShell 执行：\n%s\n", result.Release.Version, result.FinalizeCommand)
		case result.Updated:
			fmt.Fprintf(out, "已更新到 %s；上个版本保存在 .previous\n", result.Release.Version)
		case result.Release == nil:
			fmt.Fprintln(out, "当前平台尚无 CLI 发布版本")
		case result.Available:
			fmt.Fprintf(out, "发现新版本 %s（当前 %s）；运行 okuptime update 安装\n", result.Release.Version, version)
		default:
			fmt.Fprintf(out, "当前已是最新版本 %s\n", version)
		}
		return nil
	}
	if len(args) >= 2 && args[0] == "project" && args[1] == "create" {
		if len(args) != 3 || strings.TrimSpace(args[2]) == "" {
			return usageError("用法: okuptime project create NAME [--json]")
		}
		body, err := request(http.MethodPost, "/projects", nil, map[string]string{"name": args[2]})
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(out, body)
		}
		var response struct {
			Data project `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return err
		}
		fmt.Fprintf(out, "已创建项目 #%d %s\n", response.Data.ID, response.Data.Name)
		return nil
	}

	if len(args) == 2 && args[0] == "config" && args[1] == "set-token" {
		return setToken(in, out, errOut, jsonOutput)
	}
	if len(args) == 2 && args[0] == "project" && args[1] == "list" {
		body, err := request(http.MethodGet, "/projects", nil, nil)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(out, body)
		}
		var response struct {
			Data []project `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return err
		}
		for _, item := range response.Data {
			fmt.Fprintf(out, "%d\t%s\n", item.ID, item.Name)
		}
		return nil
	}
	if len(args) >= 2 && args[0] == "project" && args[1] == "monitors" {
		if len(args) < 3 {
			return usageError("用法: okuptime project monitors ID [--after-id ID] [--json]")
		}
		id, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || id <= 0 {
			return usageError("项目 ID 必须是正整数")
		}
		flags := newFlags("project monitors")
		afterID := flags.Int64("after-id", 0, "上一页的 next_cursor")
		if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 || *afterID < 0 {
			return usageError("用法: okuptime project monitors ID [--after-id ID] [--json]")
		}
		return listMonitors("/projects/"+strconv.FormatInt(id, 10)+"/monitors", *afterID, fmt.Sprintf("okuptime project monitors %d", id), jsonOutput, out, errOut)
	}
	if len(args) < 2 || args[0] != "monitor" {
		return &api.Error{Code: "usage_error", Message: "未知命令。运行 okuptime help 查看用法"}
	}

	switch args[1] {
	case "list":
		flags := newFlags("monitor list")
		afterID := flags.Int64("after-id", 0, "上一页的 next_cursor")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *afterID < 0 {
			return usageError("用法: okuptime monitor list [--after-id ID] [--json]")
		}
		return listMonitors("/monitors", *afterID, "okuptime monitor list", jsonOutput, out, errOut)
	case "add":
		flags := newFlags("monitor add")
		projectID := flags.Int64("project-id", 0, "项目 ID；省略时使用最早的项目")
		interval := flags.Int("interval", 0, "监控间隔，单位秒")
		description := flags.String("description", "", "描述")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 1 || *projectID < 0 || *interval < 0 {
			return usageError("用法: okuptime monitor add [--project-id ID] [--interval 秒] [--description 文本] URL [--json]")
		}
		payload := map[string]any{"url": flags.Arg(0)}
		if *projectID > 0 {
			payload["project_id"] = *projectID
		}
		if *interval > 0 {
			payload["monitor_interval"] = *interval
		}
		if *description != "" {
			payload["description"] = *description
		}
		body, err := request(http.MethodPost, "/monitors", nil, payload)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(out, body)
		}
		var response struct {
			Data monitor `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return err
		}
		fmt.Fprintf(out, "已添加监控 #%d %s；首次检测异步执行\n", response.Data.ID, response.Data.URL)
		return nil
	case "show", "check":
		if len(args) != 3 {
			return usageError("用法: okuptime monitor " + args[1] + " ID [--json]")
		}
		id, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || id <= 0 {
			return usageError("监控 ID 必须是正整数")
		}
		path := "/monitors/" + strconv.FormatInt(id, 10)
		method := http.MethodGet
		if args[1] == "check" {
			method, path = http.MethodPost, path+"/check"
		}
		body, err := request(method, path, nil, nil)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(out, body)
		}
		if args[1] == "check" {
			fmt.Fprintf(out, "监控 #%d 已开始重新检测；运行 okuptime monitor show %d 查看结果\n", id, id)
			return nil
		}
		var response struct {
			Data monitor `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return err
		}
		m := response.Data
		fmt.Fprintf(out, "ID: %d\n项目 ID: %d\nURL: %s\n描述: %s\n检测状态: %s\n可用性: %s\n监控间隔: %d 秒\n", m.ID, m.ProjectID, m.URL, m.Description, m.CheckStatus, m.UptimeStatus, m.MonitorInterval)
		if m.CheckedAt != nil {
			fmt.Fprintf(out, "最近检测: %s\n", *m.CheckedAt)
		}
		if m.LastStatusCode != nil {
			fmt.Fprintf(out, "HTTP 状态: %d\n", *m.LastStatusCode)
		}
		if m.LastResponseTimeMS != nil {
			fmt.Fprintf(out, "响应时间: %d ms\n", *m.LastResponseTimeMS)
		}
		return nil
	default:
		return usageError("未知命令。运行 okuptime help 查看用法")
	}
}

func listMonitors(path string, afterID int64, command string, jsonOutput bool, out, errOut io.Writer) error {
	query := url.Values{}
	if afterID > 0 {
		query.Set("after_id", strconv.FormatInt(afterID, 10))
	}
	body, err := request(http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(out, body)
	}
	var response struct {
		Data       []monitor `json:"data"`
		NextCursor *int64    `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	for _, item := range response.Data {
		fmt.Fprintf(out, "%d\t%s\t%s\t%s\n", item.ID, item.URL, item.CheckStatus, item.UptimeStatus)
	}
	if response.NextCursor != nil {
		fmt.Fprintf(errOut, "下一页: %s --after-id %d\n", command, *response.NextCursor)
	}
	return nil
}

func request(method, path string, query url.Values, payload any) ([]byte, error) {
	client, err := api.New()
	if err != nil {
		return nil, err
	}
	return client.Do(method, path, query, payload)
}

func setToken(in io.Reader, out, errOut io.Writer, jsonOutput bool) error {
	if err := config.SaveToken(in, errOut); err != nil {
		code := "config_error"
		if errors.Is(err, config.ErrInvalidToken) {
			code = "validation_failed"
		}
		return &api.Error{Code: code, Message: err.Error()}
	}
	if jsonOutput {
		fmt.Fprintln(out, `{"data":{"saved":true}}`)
	} else {
		fmt.Fprintln(out, "访问令牌已保存")
	}
	return nil
}

func newFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func usageError(message string) error { return &api.Error{Code: "usage_error", Message: message} }

func hasJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func withoutJSONFlag(args []string) []string {
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--json" {
			result = append(result, arg)
		}
	}
	return result
}

func printJSON(out io.Writer, body []byte) error {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, body, "", "  "); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, formatted.String())
	return err
}

const usage = `okuptime - Okuptime 命令行客户端

用法:
  okuptime version [--json]
  okuptime update [--check] [--json]
  okuptime config set-token
  okuptime project create NAME [--json]
  okuptime project list [--json]
  okuptime project monitors ID [--after-id ID] [--json]
  okuptime monitor add [--project-id ID] [--interval 秒] [--description 文本] URL [--json]
  okuptime monitor list [--after-id ID] [--json]
  okuptime monitor show ID [--json]
  okuptime monitor check ID [--json]

配置:
  OKUPTIME_TOKEN     可覆盖已保存的访问令牌
  OKUPTIME_BASE_URL  API 地址，默认 https://www.okuptime.com

--json 可放在命令的任意位置；输出保留 API 的 data/error 格式。
`
