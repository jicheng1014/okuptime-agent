# Okuptime Agent

Okuptime 用户 API 的 Go 命令行客户端。需要 Go 1.26。第一版管理已有项目和监控；检测为异步任务。

结构与探针一致：`cmd/okuptime` 是薄入口，`internal/cli` 负责命令与输出，`internal/api` 负责用户 API 通信，`internal/config` 负责令牌。后续 Skill 直接调用可执行文件并读取 `--json`，不需要复制鉴权或业务规则。

```sh
git clone https://github.com/jicheng1014/okuptime-agent.git
cd okuptime-agent
make install
okuptime config set-token
okuptime project list
okuptime monitor add --project-id 1 --interval 600 --description 首页 https://example.com
okuptime project monitors 1 --json
okuptime monitor show 1
okuptime monitor check 1
```

在网站“个人资料 → API 访问令牌”创建令牌。`config set-token` 从终端隐藏读取，或从标准输入读取；令牌保存在用户配置目录的 `okuptime/config.json`，文件权限为 `0600`。自动化环境可设置 `OKUPTIME_TOKEN`，它优先于本地配置。不要把令牌放入命令参数。

`--json` 可放在任意位置；成功时保留 API 的 `data` 与 `next_cursor`，API 错误时输出 `error.code`、`error.message` 和可能存在的 `error.details`，并以非零状态退出。列表每页最多 100 条；`monitor list` 列出全部监控，`project monitors ID` 列出指定项目的监控。按返回的 `next_cursor` 使用 `--after-id` 获取下一页。

默认连接 `https://www.okuptime.com`。本地开发可设置 `OKUPTIME_BASE_URL=http://localhost:3000`；远程地址必须使用 HTTPS。`monitor check` 仅提交重新检测，随后使用 `monitor show` 查询结果。

开发命令：`make test`、`make vet`、`make build`、`make build-linux`（默认 amd64，可用 `GOARCH=arm64` 覆盖）。构建产物位于 `dist/`。
