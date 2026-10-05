# Okuptime CLI & AI Skill

通过 [OK Uptime](https://www.okuptime.com) 管理项目、网站监控和检测结果。CLI 与 Codex / Claude Code Skill 在本仓库维护，使用同一个网站账号；添加监控和重新检测都是异步任务。

- [AI Skill 安装、迁移与更新](#ai-skill)
- [CLI 下载、授权与使用](#cli)
- [源码开发](#源码开发)

## AI Skill

将 [`skills/okuptime`](skills/okuptime) 完整安装到 AI 助手的 Skill 目录。只复制该子目录（`SKILL.md` 和 `scripts/`），不要将整个 CLI 仓库放入 Skill 目录，也不要只复制 `SKILL.md`。

| AI 助手 | 安装目录 |
| --- | --- |
| Codex | `~/.codex/skills/okuptime` |
| Claude Code | `~/.claude/skills/okuptime` |

可以向助手发送：

> 请从 https://github.com/jicheng1014/okuptime-agent.git 临时克隆，仅将 skills/okuptime 完整复制到当前助手的 Skill 目录。目标已存在时先检查并停止自动覆盖。阅读安装后的 SKILL.md，按当前系统安装或验证 CLI，校验平台、架构、大小和 SHA256，失败即停止。授权时提醒在本机终端执行 okuptime config set-token，不索取、读取或输出 Token。

手动安装需要 Git。macOS / Linux（Claude Code 将 `skill_root` 改成 `$HOME/.claude/skills`）：

```sh
skill_root="$HOME/.codex/skills"
if [ -e "$skill_root/okuptime" ] || [ -L "$skill_root/okuptime" ]; then
  echo 'Skill 已存在，请先按下方说明备份，再安装。'
else
  skill_checkout=$(mktemp -d)
  if git clone --depth 1 https://github.com/jicheng1014/okuptime-agent.git "$skill_checkout/repo"; then
    mkdir -p "$skill_root"
    cp -R "$skill_checkout/repo/skills/okuptime" "$skill_root/okuptime"
  fi
  rm -rf "$skill_checkout"
fi
```

Windows PowerShell（Claude Code 将 `.codex` 改成 `.claude`）：

```powershell
$skillRoot = Join-Path $HOME '.codex\skills'
$skillTarget = Join-Path $skillRoot 'okuptime'
if (Test-Path -LiteralPath $skillTarget) { throw 'Skill 已存在，请先备份再安装。' }
$skillCheckout = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
try {
    git clone --depth 1 https://github.com/jicheng1014/okuptime-agent.git $skillCheckout
    if ($LASTEXITCODE -ne 0) { throw 'Git clone failed' }
    New-Item -ItemType Directory -Path $skillRoot -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $skillCheckout 'skills\okuptime') -Destination $skillTarget -Recurse
} finally {
    if (Test-Path -LiteralPath $skillCheckout) { Remove-Item -LiteralPath $skillCheckout -Recurse -Force }
}
```

安装后开启新会话，或重新加载 / 重启助手。直接要求“把 https://example.com 加入公司项目”“查看监控 12 的状态”或“重新检测监控 12”。首次使用发现 CLI 未安装时，Skill 会调用附带安装器，使用官网预编译包；无需 Go、管理员权限或修改 PATH。已有 CLI 仅验证，不自动覆盖或升级。账号授权见 [CLI](#cli)。

### 已有安装迁移与 Skill 更新

此前从 `okuptime-skill` 安装的 Skill 仍能运行。更新时先检查并备份现有 Skill 目录（包括本地改动和原 `.git`），再执行上方安装步骤。不要对旧仓库直接更换 Git remote，也不要把 CLI 仓库克隆到旧 Skill 目录。

```sh
skill_home="$HOME/.codex" # Claude Code 使用 ~/.claude
mkdir -p "$skill_home/skill-backups"
mv "$skill_home/skills/okuptime" "$skill_home/skill-backups/okuptime-$(date +%Y%m%d%H%M%S)"
```

Windows 在 PowerShell 中：

```powershell
$skillHome = Join-Path $HOME '.codex' # Claude Code 使用 .claude
$skillBackups = Join-Path $skillHome 'skill-backups'
New-Item -ItemType Directory -Path $skillBackups -Force | Out-Null
Move-Item -LiteralPath (Join-Path $skillHome 'skills\okuptime') -Destination (Join-Path $skillBackups ('okuptime-' + (Get-Date -Format 'yyyyMMddHHmmss')))
```

备份保存在 Skill 搜索目录外，避免助手发现重复 Skill。确认新安装正常后可保留备份；失败时恢复旧目录。迁移不修改 CLI 或账号配置。**CLI 的 `update` 不会更新 Skill，更新 Skill 也不会升级已有 CLI。**之后仍按“备份旧目录 → 临时克隆 → 仅复制 `skills/okuptime` → 重新加载助手”更新。

## CLI

从官网 [CLI 下载页](https://www.okuptime.com/cli) 选择操作系统和架构。无需编译；支持以下六个平台。元数据 API 无需 Token，返回当前版本的 `url`、`sha256`、`size` 等校验信息；暂无对应发布时返回 404。

| 系统 / 芯片 | platform / architecture | 最新版本与下载地址 |
| --- | --- | --- |
| macOS Intel | darwin / amd64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=darwin&architecture=amd64) |
| macOS Apple Silicon | darwin / arm64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=darwin&architecture=arm64) |
| Linux x86_64 | linux / amd64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=linux&architecture=amd64) |
| Linux ARM64 | linux / arm64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=linux&architecture=arm64) |
| Windows Intel / AMD | windows / amd64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=windows&architecture=amd64) |
| Windows ARM64 | windows / arm64 | [发布信息](https://www.okuptime.com/api/v1/cli/releases/latest?platform=windows&architecture=arm64) |

### 安装与校验

推荐让已安装的 Skill 执行安装器，也可从仓库根目录运行：

```sh
sh skills/okuptime/scripts/install.sh
```

Windows PowerShell：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File skills/okuptime/scripts/install.ps1
```

WSL 使用 Linux 安装器。macOS / Linux 需要 curl 和 SHA256 工具；Windows 使用系统 PowerShell。安装器拒绝重定向和非官网地址，检查平台、架构、大小（最大 64 MiB）、SHA256 和可执行文件版本，全部通过后才安装，不覆盖已有 CLI。首次安装通过官网 HTTPS 和发布校验值建立信任，后续更新还验证内置公钥的发布签名。Windows 的执行策略参数仅影响本次进程。

| 系统 | 安装路径 | 验证命令 |
| --- | --- | --- |
| macOS / Linux | `~/.local/bin/okuptime` | `~/.local/bin/okuptime version --json` |
| Windows | `%LOCALAPPDATA%\OKUptime\bin\okuptime.exe` | `& "$env:LOCALAPPDATA\OKUptime\bin\okuptime.exe" version --json` |

未加入 PATH 时，后续命令均使用表中的绝对路径；PowerShell 使用 `&` 调用。也可自行将该用户目录加入 PATH。

手动下载时，先从上表对应的官方元数据中读取 `data.url`、`data.size`、`data.sha256`，下载该 URL 的文件并核对大小和摘要。macOS 使用 `shasum -a 256 ./okuptime`，Linux 使用 `sha256sum ./okuptime`，Windows 使用 `Get-FileHash .\okuptime.exe -Algorithm SHA256`。校验失败停止安装，不跳过校验或申请管理员权限。校验通过后，macOS / Linux 可执行 `chmod +x ./okuptime` 并放入用户目录；Windows 保留 `.exe` 后缀并放入上表目录。目标已存在时优先使用 CLI 更新命令。

### 本机授权

在官网“个人资料 → [API 访问令牌](https://www.okuptime.com/api_tokens)”创建 Token，然后在本机终端执行：

```sh
okuptime config set-token
```

命令隐藏读取凭证。**不要将 Token 发到 AI 聊天、放入命令参数、日志、Skill 或仓库。**自动化环境可通过受保护的环境变量 `OKUPTIME_TOKEN` 提供凭证，它优先于本地配置。不要读取或输出已经保存的 Token。

凭证保存于系统用户配置目录的 `okuptime/config.json`；Unix 文件权限为 `0600`，Windows 使用 `%APPDATA%\okuptime\config.json` 并继承用户目录权限。重新运行 `config set-token` 可更换凭证；鉴权失败时配置或更换 Token，不反复重试无效凭证。

### 使用

```sh
okuptime project list --json
okuptime project create '生产环境' --json
okuptime monitor add --project-id 1 --interval 600 --description '首页' https://example.com --json
okuptime project monitors 1 --json
okuptime monitor list --json
okuptime monitor show 1 --json
okuptime monitor check 1 --json
```

`monitor list` 列出账号全部监控，`project monitors ID` 列出指定项目。每页最多 100 条；使用返回的 `next_cursor` 作为 `--after-id` 获取下一页。`--json` 可放在任意位置，成功保留 API 的 `data` / `next_cursor`，失败输出 `error.code`、`error.message` 和可能存在的 `error.details`，以非零状态退出。

添加成功已触发首次检测，无需再调用 `monitor check`。该命令只提交重新检测；随后用 `monitor show` 查看状态和检测时间，任务受理不代表网站正常。默认连接 `https://www.okuptime.com`；明确使用本地开发环境时可设置 `OKUPTIME_BASE_URL=http://localhost:3000`，远程地址必须使用 HTTPS。

### CLI 更新

```sh
okuptime version --json
okuptime update --check --json
okuptime update --json
```

版本和更新检查不需要 Token。`--check` 只检查，`update` 明确下载并安装。业务命令最多每 24 小时检查一次更新（最长等待 2 秒），仅向 stderr 提示新版，不影响 JSON 或业务结果。检查缓存位于用户缓存目录 `okuptime/update-check`，与凭证分开。

更新校验 Ed25519 签名、SHA256 和实际大小。macOS / Linux 原子替换可执行文件，保留旁边的 `.previous`；符号链接安装更新实际目标。用户配置不变。目录无写权限时报告错误，不自动调用 sudo。

Windows 返回 `data.staged=true`、`data.updated=false`、`staged_path` 和 `finalize_command`，表示下载验签完成，尚未替换旧程序。关闭其他 CLI 实例，等当前命令退出，再将本机 CLI 返回的 `finalize_command` 作为单个参数传给 `powershell.exe -NoProfile -NonInteractive -Command` 执行；不要从远程元数据拼接执行命令。第二步再次校验 SHA256、备份 `.previous`、替换并检查版本，失败时恢复备份。直接使用 CLI 时也可执行非 JSON 输出中给出的 PowerShell 命令；Skill 在用户要求更新时自动完成这一步。最后运行 `version --json` 核对版本。

### 常见问题

- **助手未发现 Skill：**检查实际目录下有 `SKILL.md` 和两个安装器，开启新会话或重新加载助手；整个 CLI 仓库不能作为 Skill 安装。
- **找不到 `okuptime`：**使用上表的绝对路径；PowerShell 使用 `&`，不要为了 PATH 重新安装。
- **不支持平台 / 元数据 404：**核对系统、架构和官网发布状态，停止安装并报告，不擅自改为源码编译。
- **下载失败 / 大小或摘要不符：**检查官网和网络；不要跳过 HTTPS、SHA256 或签名检查，也不要执行未校验的文件。
- **401 / unauthorized：**在本机重新运行 `config set-token`，核对账号和 Token 状态；不要将 Token 交给助手排查。
- **配额、重复或限流：**根据 JSON 的 `error.code` 处理；重复监控不删除重建，网络超时先查询项目再重试，避免重复创建。
- **Windows 更新停在 staged：**完成 CLI 给出的 PowerShell 替换步骤并核对版本，下载完成不等于更新完成。
- **开发构建无法原位更新：**`dev` 或缺少发布公钥的构建只能检查版本，安装官网签名构建后再使用自更新。

## 源码开发

需要 Go 1.26。`cmd/okuptime` 是入口，`internal/cli` 负责命令和输出，`internal/api` 负责用户 API，`internal/config` 负责凭证。Skill 仅调用 CLI 的 JSON 接口，不复制鉴权或业务规则。

```sh
git clone https://github.com/jicheng1014/okuptime-agent.git
cd okuptime-agent
make test
make vet
make build
make install
python3 tests/skill_install_test.py
```

构建产物位于 `dist/`。`make build-linux` / `make build-windows` 默认 amd64，可用 `GOARCH=arm64` 覆盖。官方构建通过 `main.version`、`main.updatePublicKey` 注入版本和 Base64 Ed25519 PKIX DER 公钥，例如 `make build VERSION=1.0.0 UPDATE_PUBLIC_KEY=...`。签名 payload 为 `version\nplatform\narchitecture\nurl\nsha256\nsize\n`。开发构建默认 `dev`，未配置发布公钥或使用 `dev` 时拒绝安装更新。

## Release automation

Push a `vX.Y.Z` tag on a commit already included in `main` to publish a signed six-platform release to OK Uptime. Main pushes and pull requests run tests, vet and a build without publishing. `.github/workflows/cli.yml` requires repository Secrets `CLI_UPDATE_PRIVATE_KEY` (the existing Ed25519 PEM) and `CLI_RELEASE_UPLOAD_TOKEN` (the dedicated server upload token). Keep the existing signing key so installed clients trust later versions. The workflow verifies live metadata and every downloaded binary after uploading.

For a local release: `ruby script/release.rb release X.Y.Z /outside/repository/cli-update.pem`. Output defaults to `dist/releases`; the signing key must be outside the repository with permissions `0600`.
