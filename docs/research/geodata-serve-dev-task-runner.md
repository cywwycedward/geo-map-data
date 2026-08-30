# geodata-serve 开发命令与一键启动调研

> 调研日期：2026-08-30。范围：为同一仓库中的 Go 服务和 Node 原生测试台统一开发、测试、初始化与本地一键启动命令；目标环境为 Windows。

## 问题与本地事实

仓库当前有一个 Go module（[`services/geodata-serve/go.mod`](../../services/geodata-serve/go.mod)）和一个零依赖、无构建步骤的 Node 原生测试台（[`apps/geodata-serve-web/README.md`](../../apps/geodata-serve-web/README.md)）。服务 CLI 要求调用方显式提供 `--database`、`--runtime-dir`、`--backup-dir` 和 `--working-dir`；测试台通过 runtime 目录中的 `server.json` 发现服务。这意味着启动器必须编排有严格先后关系的步骤，但不能把机器/项目路径写死在已提交文件中。

## 已验证的工具能力

| 方案 | 适合之处 | 对本仓库的限制与结论 |
| --- | --- | --- |
| 根 `package.json` 的 npm scripts | npm 的 `scripts` 支持任意命令，可由 `npm run <name>` 运行；可为开发者提供熟悉的短命令。[npm scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/) | npm workspaces 的定义是由一个顶层 Node package 管理本地多个 Node packages，并解决它们的链接与依赖；它不管理 Go module 或服务就绪状态。[npm workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) 因此可把根 `package.json` 作为**任务工具的可复现安装入口/别名**，但不应把本仓库声明为 npm workspace。 |
| Taskfile（go-task） | Task 的任务可设置工作目录、平台条件、变量和 `.env` 文件；`task --list` 能列出带描述的任务，适合成为跨语言命令目录。[Task guide](https://taskfile.dev/docs/guide) [Task reference](https://taskfile.dev/docs/reference/schema) 官方同时提供 Windows 的安装方式，并明确 npm 可将 `@go-task/cli` 安装为项目依赖。[Task installation](https://taskfile.dev/docs/installation) | `deps` 始终并行；官方建议必须串行时在 `cmds` 中显式调用 task。[Task dependencies](https://taskfile.dev/docs/guide#task-dependencies) 所以不能用 `deps` 表达“先 init、再服务 ready、后测试台”，也不应让两个常驻进程分别成为依赖。Task 适合做命令目录，而非本项目的完整进程监督器。 |
| GNU Make | Make 能表达文件生成依赖并通过 `-j` 并发执行 recipes。[GNU Make parallel execution](https://www.gnu.org/software/make/manual/html_node/Parallel.html) | GNU Make 在 Windows/MS-DOS 中的 shell 选择与其他系统不同且更复杂。[GNU Make: choosing the shell](https://www.gnu.org/software/make/manual/html_node/Choosing-the-Shell.html) 本项目以 PowerShell/Windows 路径为主，且任务主要是进程编排而非增量文件构建，故不作为首选。 |
| just | `justfile` 是轻量命令运行器；Windows 可以显式配置 PowerShell 或 `cmd.exe`，且可按 OS 条件定义 recipe。[just prerequisites](https://just.systems/man/en/prerequisites.html) [just platform attributes](https://just.systems/man/en/enabling-and-disabling-items.html) | 未配置时 Windows 仍需要 Git for Windows、GitHub Desktop 或 Cygwin 提供的 `sh`；也需要团队另行安装 `just`。[just prerequisites](https://just.systems/man/en/prerequisites.html) 它是可行备选，但相对 Task 没有足以抵消额外 shell 约定的优势。 |
| Docker Compose | Compose 用一份 YAML 定义、启动和管理多容器应用；`depends_on` 可配合 healthcheck 表达依赖服务的就绪顺序。[Compose overview](https://docs.docker.com/compose/) [Compose startup order](https://docs.docker.com/compose/how-tos/startup-order/) | 它解决的是多**容器**应用。本服务的数据库、运行目录、备份目录和工作目录都属于调用方本机路径，容器化后必须逐一做 bind mount；Docker 文档说明 bind mount 与宿主目录结构强绑定，换主机时可能失败，Docker Desktop 还通过 Linux VM 处理原生 Windows 路径。[Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/) 仅为串联两个本地进程引入 Compose 会增加环境边界，当前不值得。 |

## 推荐（基于上述事实的工程判断）

采用“两层但职责清晰”的方案：**根 `Taskfile.yml` 是统一命令目录；一个很小的 Node 标准库 supervisor 是唯一的多进程生命周期所有者；根私有 `package.json` 只固定并暴露 Task。**

这不是 npm workspaces。Node 在测试台中本来就是必需运行时；Task 官方支持作为项目 npm 依赖安装，因此把它锁定在根 `package-lock.json` 可以避免要求每位开发者预先全局安装 task。[Task installation](https://taskfile.dev/docs/installation) Go 与 Node 仍各自保留原有 module/package 边界。

建议的提交结构如下（此调研不实施）：

```text
Taskfile.yml                         # 唯一、可发现的跨语言命令目录
package.json + package-lock.json     # private；只含开发任务入口和固定的 @go-task/cli
scripts/geodata-dev.mjs              # Node 标准库：init/dev/test 等的有状态编排
geodata-serve.local.example.json     # 可提交的字段说明，不含真实本地路径
geodata-serve.local.json             # .gitignore：每个项目/机器的实际绝对路径
```

`Taskfile.yml` 应只包含短而稳定的任务名，例如 `geodata:init`、`geodata:dev`、`geodata:test`、`geodata:test-go`、`geodata:test-web`、`geodata:build`。每个任务显式指定其工作目录：Go 检查在 `services/geodata-serve/`，Node 测试在 `apps/geodata-serve-web/`。Task 的 `dir`、平台条件和任务描述正是为此类任务目录设计的。[Task guide](https://taskfile.dev/docs/guide)

其中 `geodata:dev` 只调用一次 `node scripts/geodata-dev.mjs dev --config <local-config>`。supervisor 负责以下顺序和退出清理：

1. 读取未提交的配置，验证四个服务路径和 web port 都存在；绝不从当前目录、用户目录或环境隐式猜测路径。
2. 构建 Go 二进制到已忽略的临时输出目录，运行 `init`，再启动 `serve`。
3. 等待 `server.json` 出现且服务 health 检查成功后，才启动 `apps/geodata-serve-web/server.mjs`。
4. 在 Ctrl+C、前置步骤失败或任一子进程退出时，关闭 web 子进程，并经公开的服务关闭接口停止 Go 服务、等待子进程退出；不得打印 `server.json` 中的 token。

把这段逻辑放在单个 Node 进程而不是 Task 的多条 `cmds` 中，是因为 Task 官方说明每条命令在单独 shell 中运行；对复杂多行工作建议移到单独文件。[Task FAQ](https://taskfile.dev/docs/faq) 此处的“拥有两个 child process、轮询 readiness、捕获 Ctrl+C、按序清理”正是需要同一进程持有状态的逻辑。Node supervisor 不需要第三方依赖。

建议的用户体验是先一次 `npm install`，然后用一个入口运行 `npm run dev`（或文档选择的等价 `npm exec task geodata:dev`）。常规验证可分别运行 `npm run test:go`、`npm run test:web` 和 `npm run test`。实际命令名在实施时应与现有 README 保持一致，且先用真实 Windows CGO 环境验证。

## 配置与安全边界

本地配置只保存服务已经要求显式传入的四个路径和可选 web 端口，例如：

```json
{
  "database": "D:\\geo\\data.duckdb",
  "runtimeDir": "D:\\geo\\runtime",
  "backupDir": "D:\\geo\\backups",
  "workingDir": "D:\\geo\\project",
  "webPort": 8787
}
```

真实配置必须被 `.gitignore` 忽略；示例配置不可带个人绝对路径、token 或数据库副本。token 继续由服务写入 `server.json`，测试台/启动器只在运行时读取它，不把它转存到配置、日志或 npm 环境变量。

## 不现在采用 Compose 的条件与重新评估触发点

当前先不引入 Compose；这不表示它不可用。以下任一条件出现时应重新评估：团队要求在 CI 与开发机使用同一 Linux 容器运行时、服务需与真实的外部容器依赖一起启动、或需要可发布/可部署的整套容器环境。届时应显式设计四个路径的 bind mount、Docker Desktop/WSL 文件系统语义和 extension 缓存卷，再利用 Compose 的 service healthcheck/依赖能力；不要把本地开发快捷方式误当成无需宿主存储设计的容器化方案。[Compose overview](https://docs.docker.com/compose/) [Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)

## 来源

- [npm scripts](https://docs.npmjs.com/cli/v11/using-npm/scripts/)
- [npm workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/)
- [Task guide](https://taskfile.dev/docs/guide)
- [Task installation](https://taskfile.dev/docs/installation)
- [Task FAQ](https://taskfile.dev/docs/faq)
- [GNU Make manual: choosing the shell](https://www.gnu.org/software/make/manual/html_node/Choosing-the-Shell.html)
- [GNU Make manual: parallel execution](https://www.gnu.org/software/make/manual/html_node/Parallel.html)
- [just manual: prerequisites](https://just.systems/man/en/prerequisites.html)
- [just manual: platform attributes](https://just.systems/man/en/enabling-and-disabling-items.html)
- [Docker Compose documentation](https://docs.docker.com/compose/)
- [Docker Compose startup order](https://docs.docker.com/compose/how-tos/startup-order/)
- [Docker bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)
