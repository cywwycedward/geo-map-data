# Windows Go/CGO 最佳实践（2026）

> 调研日期：2026-08-28。目标环境：Windows amd64、Go 1.26.5、`github.com/duckdb/duckdb-go/v2 v2.5.6`（DuckDB 1.4.5）。

## 结论

本项目应使用官方 Windows Go 安装包，并使用 MSYS2 的 UCRT64 GCC 提供 CGO 编译器、Windows 头文件、链接器和运行库。Go 构建命令在 PowerShell 中运行；MSYS2 只提供 C 工具链，不替换 Go 工具链。

不要把当前单独的 LLVM clang 当作可直接替代方案，也不要混用 MSVC、MSYS/Cygwin、MINGW64、UCRT64 和 CLANG64 的对象文件或 C++ 运行库。MSYS2 官方建议不确定时选择 UCRT64；它使用 x86_64、UCRT 和 GCC。DuckDB Go 驱动的 Windows 安装说明也明确要求正确版本的 GCC 和运行库，并以 MSYS2 的 UCRT64 GCC 为示例。

## 为什么需要 CGO

CGO 允许 Go 调用 C 代码。`duckdb-go` 通过 CGO 包装 DuckDB C API；其绑定模块要求 `CGO_ENABLED=1` 和系统 C 编译器。驱动默认使用适配平台的预编译静态库，因此不需要在本项目中启用自定义 DuckDB 链接 build tag，也不需要启用 `duckdb_arrow`。

Go 官方说明：本机编译在编译器可用时通常默认启用 CGO；如果 PATH 找不到默认 C 编译器，CGO 会被关闭。设置 `CGO_ENABLED=1` 只能打开构建路径，不能替代 GCC、头文件、链接器和运行库的安装。

## 推荐安装

1. 从 [MSYS2 官方安装说明](https://www.msys2.org/docs/installer/) 安装 x86_64 MSYS2。
2. 打开 MSYS2 UCRT64 终端。MSYS2 的 [环境说明](https://www.msys2.org/docs/environments/) 将 UCRT64 定义为 x86_64 + GCC + UCRT，并建议不确定时选择它。
3. 按 [MSYS2 更新说明](https://www.msys2.org/docs/updating/) 完成滚动发行版的完整升级。
4. 在 UCRT64 终端安装工具链：

   ```bash
   pacman -S --needed mingw-w64-ucrt-x86_64-toolchain
   ```

   如只需最小 GCC，可安装 [官方 `mingw-w64-ucrt-x86_64-gcc` 包](https://packages.msys2.org/packages/mingw-w64-ucrt-x86_64-gcc)；完整 toolchain 更适合作为 CGO 开发环境，因为它同时提供 GCC 依赖的 binutils、CRT、headers 和运行库。

## PowerShell 构建配置

以下变量只作用于当前 PowerShell 会话，不把个人绝对路径写入仓库：

```powershell
$env:CGO_ENABLED = "1"
$env:CC = "C:\msys64\ucrt64\bin\gcc.exe"
$env:CXX = "C:\msys64\ucrt64\bin\g++.exe"
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"

gcc --version
g++ --version
go version
go env GOOS GOARCH CGO_ENABLED CC CXX
```

Go 官方 [cgo 文档](https://pkg.go.dev/cmd/cgo)确认 `CC`/`CXX` 可分别指定 C/C++ 编译器，`CGO_ENABLED=1` 可显式启用 CGO。DuckDB 官方 [v2.5.6 README](https://github.com/duckdb/duckdb-go/blob/v2.5.6/README.md)确认 Windows 需要正确版本的 GCC 和运行库，并示例使用 `C:\msys64\ucrt64\bin` 加入 PATH。

验证顺序：

```powershell
where.exe gcc
gcc --version
go env GOOS GOARCH CGO_ENABLED CC CXX
go mod download
go mod verify
go test ./...
go vet ./...
go build ./cmd/geodata-serve
```

本项目默认不使用 `-tags=duckdb_arrow`。DuckDB Go v2 将 Arrow 接口设为 opt-in；只有显式传入该 tag 才启用它。默认静态库路径是本项目所需路径，除非主动切换到自定义库，否则不要设置 `duckdb_use_static_lib`、`duckdb_use_lib` 或自定义 `CGO_LDFLAGS`。

## 不推荐的组合

- 仅设置 `CGO_ENABLED=1` 而没有 GCC：会得到 `C compiler ... not found`。
- 直接使用 MSVC-target 的 LLVM clang：Go 的 Windows CGO 参数和 DuckDB 预编译库需要匹配的 GNU/UCRT 工具链；缺失 Windows SDK/MinGW sysroot 时会出现 `windows.h` 等头文件错误。
- 把 MSYS/Cygwin 的 `/usr/bin` GCC 当作 MinGW UCRT64 GCC。Go 官方资料明确不支持 Cygwin 工具链；MSYS2 官方也区分 MSYS、UCRT64 和 CLANG64 的 CRT/标准库。
- 混用 UCRT64 和 CLANG64 的 C++ 运行库。MSYS2 说明不同 CRT/运行库的对象和静态库不应混合。
- 为绕过下载问题设置 `GOSUMDB=off` 或 `GOINSECURE`。这会削弱模块完整性或传输安全，不是普通网络重试方案。

## Go module 下载故障

Go 官方 [Modules Reference](https://go.dev/ref/mod) 说明 `GOPROXY` 是逗号或竖线分隔的代理列表：逗号通常只在 404/410 时回退，竖线可在任意错误（包括网络错误）时回退。对于本项目的公开依赖，可先在当前会话使用：

```powershell
$env:GOPROXY = "https://proxy.golang.org,direct"
go mod download
```

若代理出现 `unexpected EOF` 等临时网络错误，可改用更积极回退的：

```powershell
$env:GOPROXY = "https://proxy.golang.org|direct"
go mod download
go mod tidy
go mod verify
```

保持默认 checksum 校验（通常 `GOSUMDB=sum.golang.org`），不要关闭校验。若组织有可信内部 Go proxy，应优先使用组织 proxy，并让它对公共模块提供缓存；不要把个人代理地址、token 或凭据提交到仓库。

## 本次环境的对应诊断

此前默认 UCRT64 GCC 16.2 与 `duckdb-go-bindings v0.3.5` 的预编译 Windows 静态库链接时，因缺少 `__emutls_v._ZSt11__once_call` 和 `__emutls_v._ZSt15__once_callable` 失败。不要通过混用 GCC 16 编译器与 GCC 15 运行库或添加链接 shim 规避该 ABI 问题。

当前默认 PowerShell 自动从配置的 UCRT64 工具链目录发现 GCC 15.2.0 及匹配的 C++ 运行库。无需设置 `CC`、`CXX`、`CGO_CFLAGS` 或 `CGO_LDFLAGS`，本项目已实际通过 `go test -count=1 ./...`、`go vet ./...`、`go mod verify`、`go build ./cmd/geodata-serve`，并执行 `init`、真实 DuckDB 查询和 `/shutdown` 生命周期验证。MSYS2 滚动升级改变 GCC 主版本时，须重新完成这套验证，不应假设新版本保持二进制兼容。

## Sources

- [Go cgo command documentation](https://pkg.go.dev/cmd/cgo)
- [Go Modules Reference](https://go.dev/ref/mod)
- [DuckDB Go driver v2.5.6 README](https://github.com/duckdb/duckdb-go/blob/v2.5.6/README.md)
- [DuckDB Go bindings README](https://github.com/duckdb/duckdb-go-bindings)
- [MSYS2 Environments](https://www.msys2.org/docs/environments/)
- [MSYS2 Installer](https://www.msys2.org/docs/installer/)
- [MSYS2 Updating](https://www.msys2.org/docs/updating/)
- [MSYS2 package: mingw-w64-ucrt-x86_64-gcc](https://packages.msys2.org/packages/mingw-w64-ucrt-x86_64-gcc)
