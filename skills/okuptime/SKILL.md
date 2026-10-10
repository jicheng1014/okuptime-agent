---
name: okuptime
description: 通过 OK Uptime CLI 将网站加入账号的监控项目，查询项目、监控及检测结果，发起重新检测，并检查或按用户要求升级 CLI。用户提到 OK Uptime、添加网站监控或批量导入网站时使用。
---

# OK Uptime

使用 `okuptime` CLI 操作用户自己的 OK Uptime 账号。所有业务调用使用 `--json`，按退出状态及 `error.code` 判断结果；不要复制 API 鉴权和业务逻辑。

## 安装与账号

用户要求使用 OK Uptime 管理网站时，先检查 CLI，未安装则直接运行本 Skill 附带的安装器；首次安装是完成本次任务的准备步骤，无需让用户自行下载、编译或修改 PATH。已有 CLI 不自动升级，不覆盖其他文件。

按当前执行环境选择安装器（WSL 使用 Linux 安装器）：

| 系统 | 安装命令（路径相对于本 Skill 目录） | 安装位置 |
| --- | --- | --- |
| macOS | `sh scripts/install.sh` | `~/.local/bin/okuptime` |
| Linux | `sh scripts/install.sh` | `~/.local/bin/okuptime` |
| Windows | `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1` | `%LOCALAPPDATA%\OKUptime\bin\okuptime.exe` |

实际执行时使用安装器的绝对路径，不能假设当前目录是 Skill 目录。Windows 的 ExecutionPolicy 参数只作用于本次进程，不更改系统策略。macOS/Linux 需要 curl 和系统 SHA256 工具；Windows 使用自带 PowerShell，不需要 Python、Ruby 或 Go。支持 amd64 和 arm64；不支持的平台或官网无发布包时停止并报告原因，不擅自改为源码编译。

先查找 PATH 中的 `okuptime`；再检查表中的用户目录。安装后用可执行文件绝对路径运行所有命令，无需更改 PATH。PowerShell 调用带空格的路径使用 `&`，例如 `& "$env:LOCALAPPDATA\OKUptime\bin\okuptime.exe" version --json`。

安装器从官网 HTTPS 元数据接口读取最新版本，拒绝重定向与非官方地址，限制下载大小并校验平台、大小和 SHA256；检查下载的二进制能运行后才安装。首次安装通过官网 HTTPS 和发布校验值建立信任；后续 CLI 更新还会验证内置公钥的发布签名。安装失败报告真实原因，不自动使用 sudo、管理员权限或绕过校验。

先运行 `okuptime project list --json` 检查授权。缺少或失效的 Token 会在 `error.details.token_url` 返回创建令牌的页面地址；向用户展示可点击链接，引导登录并创建 Token。旧版 CLI 未返回 URL 时使用 [API 访问令牌](https://www.okuptime.com/api_tokens)；用户明确指定其他环境时使用该环境的 `/api_tokens` 页面。

用户可以在对话中提供 Token，由助手通过标准输入交给 `okuptime config set-token --json` 保存；先简短说明 Token 会保留在聊天记录中。也可由用户在本机终端执行 `okuptime config set-token`，交互输入不会回显。不要将 Token 放进命令参数、回显、日志、Skill 或仓库；使用不会回显输入的执行方式，工具无法做到时改用本机终端录入。不要读取或输出已保存的 Token。自动化环境已有 `OKUPTIME_TOKEN` 时直接复用，它优先于本地配置。

保存成功后再次运行 `okuptime project list --json`，授权有效才继续原任务；保存成功不代表授权通过。鉴权失败时引导更新凭证，不反复重试无效 Token；若环境变量提供了失效 Token，提示更新该变量。默认连接官网；仅在用户明确指定环境时使用 `OKUPTIME_BASE_URL`。

## 添加网站

用户要求添加网站即授权本次添加，不必再次确认同一操作。保持用户指定的网址、端口、项目、描述和监控间隔，不擅自购买套餐或更改通知设置。

```sh
okuptime project list --json
okuptime project create '网站监控' --json
okuptime project monitors 1 --json
okuptime monitor add --project-id 1 --description '首页' https://example.com --json
```

- 指定项目时先根据列表解析 ID；只有一个项目时直接使用；多个项目且归属不明时询问用户，不默默使用最早的项目。
- 没有项目时为本次网站添加创建“网站监控”项目；用户指定新项目名时使用该名称。
- 每次添加显式传 `--project-id`。指定间隔时使用 `--interval 秒`；没有指定时保留服务端默认值。
- 多个网站逐个添加；列表有 `next_cursor` 时用 `--after-id` 翻页。`project monitors ID` 按项目查询，`monitor list` 查询账号全部监控。
- `duplicate_monitor` 记为已存在，不删除重建。重复判定按同项目的域名和端口，不把同域名不同路径误报成新增监控。
- `validation_failed` 或套餐配额不足时报告具体失败原因；`rate_limited` 时有限退避，仍失败则保留已完成项并报告剩余项。
- 网络超时可能已经创建成功。重试前查询目标项目，避免把未知结果直接当成失败。

添加成功后汇总新增、已存在和失败的网站及项目。监控创建与检测完成分开表述；添加已触发首次异步检测，不再额外调用 `monitor check`。

## 查询与重新检测

```sh
okuptime monitor list --json
okuptime monitor show 1 --json
okuptime monitor check 1 --json
```

用户要求重新检测时再使用 `monitor check`。提交成功只是任务已接受；随后通过 `monitor show` 读取 `check_status`、`uptime_status` 和检测时间。需要等待时每 5 秒查一次、最多等待 60 秒，并遵守接口限流；仍在检测则报告监控 ID 和当前状态，不声称网站正常。

## CLI 更新

```sh
okuptime version --json
okuptime update --check --json
okuptime update --json
```

发现新版先提示可更新及版本号；只有用户要求安装或更新 CLI 时执行 `update`。普通业务命令的新版提示在 stderr，stdout 仍是 JSON。版本检查无需 Token，检查失败不能阻断网站操作。

Windows 更新可能返回 `data.staged=true`、`staged_path` 和 `finalize_command`，此时尚未替换旧程序。用户已要求更新时，等 CLI 进程退出，再将 `finalize_command` 作为单个参数传给 `powershell.exe -NoProfile -NonInteractive -Command` 同步执行；该命令来自本机官方 CLI，不从远程元数据读取或拼接执行。完成后再次运行 `version --json` 核对目标版本，成功前不报告已更新。macOS/Linux 更新直接完成替换。

升级失败保留旧版本并报告原因，不自动使用 sudo。源码开发构建缺少发布公钥时不能原位升级，应安装官网签名构建。升级不修改账号配置。此 Skill 与 CLI 在 [okuptime-agent](https://github.com/jicheng1014/okuptime-agent/tree/main/skills/okuptime) 同仓库维护。CLI 升级不会更新 Skill，仅更新 Skill 也不会升级已有 CLI；按 [README 的迁移和更新说明](https://github.com/jicheng1014/okuptime-agent#ai-skill) 更新完整的 `skills/okuptime` 目录。
