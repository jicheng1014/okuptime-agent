# Okuptime CLI

Okuptime 用户 API 的 Go 命令行客户端。需要 Go 1.26。管理项目和监控；检测为异步任务。

结构与探针一致：`cmd/okuptime` 是薄入口，`internal/cli` 负责命令与输出，`internal/api` 负责用户 API 通信，`internal/config` 负责令牌。后续 Skill 直接调用可执行文件并读取 `--json`，不需要复制鉴权或业务规则。

```sh
git clone https://github.com/jicheng1014/okuptime-agent.git
cd okuptime-agent
make install
okuptime config set-token
okuptime project create "生产环境"
okuptime project list
okuptime monitor add --project-id 1 --interval 600 --description 首页 https://example.com
okuptime project monitors 1 --json
okuptime monitor show 1
okuptime monitor check 1
```

在网站“个人资料 → API 访问令牌”创建令牌。`config set-token` 从终端隐藏读取，或从标准输入读取；令牌保存在用户配置目录的 `okuptime/config.json`，Unix 文件权限为 `0600`，Windows 保存于 `%APPDATA%\okuptime\config.json` 并继承用户配置目录权限。自动化环境可设置 `OKUPTIME_TOKEN`，它优先于本地配置。不要把令牌放入命令参数。

`--json` 可放在任意位置；成功时保留 API 的 `data` 与 `next_cursor`，API 错误时输出 `error.code`、`error.message` 和可能存在的 `error.details`，并以非零状态退出。列表每页最多 100 条；`monitor list` 列出全部监控，`project monitors ID` 列出指定项目的监控。按返回的 `next_cursor` 使用 `--after-id` 获取下一页。

默认连接 `https://www.okuptime.com`。本地开发可设置 `OKUPTIME_BASE_URL=http://localhost:3000`；远程地址必须使用 HTTPS。`monitor check` 仅提交重新检测，随后使用 `monitor show` 查询结果。

开发命令：`make test`、`make vet`、`make build`、`make build-linux`（默认 amd64，可用 `GOARCH=arm64` 覆盖）。构建产物位于 `dist/`。


```sh
okuptime version
okuptime update --check --json
okuptime update
```

`version` 和 `update --check` 无需访问令牌。`update --check` 只检查；`update` 显式下载并安装（Windows 由 Skill 在命令退出后完成替换）。成功的项目/监控命令最多每 24 小时检查一次更新（最长等待 2 秒），仅向标准错误输出升级提示，失败不影响业务结果或 JSON。检查缓存位于用户缓存目录 `okuptime/update-check`，与访问令牌配置分开。

更新元数据来自 `GET /api/v1/cli/releases/latest?platform=darwin&architecture=arm64`，平台参数自动使用当前系统。`--json` 输出 `data.current_version`、`available`、`updated` 和 `release`；尚无发布时 `release` 为 `null`。支持 macOS/Linux/Windows 的 amd64/arm64。Windows 运行中的 exe 无法直接覆盖，采用以下分阶段更新。

安装前验证 Ed25519 发布签名、SHA256 和实际文件大小（最大 64 MiB）；下载与重定向都只能使用网站同域 HTTPS 地址，本机开发可使用 HTTP。下载超时 2 分钟。macOS/Linux 验证成功后原子替换可执行文件，保留旁边的 `.previous`，并保留可执行权限；符号链接安装更新其实际目标。访问令牌不变。目录权限不足会报错，由有权限的用户手动安装，CLI 不会自动调用 sudo。

官方构建通过 `main.version` 和 `main.updatePublicKey` 注入版本与 Base64 编码的 Ed25519 PKIX DER 公钥；可用 `make build VERSION=1.0.0 UPDATE_PUBLIC_KEY=...`。开发构建默认为 `dev`，可以检查版本，但未配置公钥或使用 `dev` 时拒绝安装。签名 payload 为 `version\nplatform\narchitecture\nurl\nsha256\nsize\n`，与探针发布校验一致。

Windows 的 `okuptime update --json` 先下载并验证，保存旁边的 `.pending.exe`，返回 `data.staged=true`、`data.updated=false`、`data.staged_path` 和 `data.finalize_command`。下载完成不代表已经更新。命令退出后，通过 PowerShell 执行 `finalize_command`：再次校验 SHA256、备份 `.previous`、替换 exe、执行 `version --json`；替换或版本检查失败时恢复备份。Skill 在用户授权更新后自动执行这个第二步；直接使用 CLI 的用户也可复制非 JSON 输出中的 PowerShell 命令。关闭仍在运行的其他 CLI 实例后再完成替换。

Windows 首次安装由 Skill 放入 `%LOCALAPPDATA%\OKUptime\bin\okuptime.exe`；默认用户配置和检查缓存分别使用 `%APPDATA%` 与 `%LOCALAPPDATA%`，不依赖 Unix HOME。跨平台开发可使用 `make build-windows GOARCH=amd64` 或 `GOARCH=arm64`。
