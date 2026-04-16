# ModuleBalancingClient

`ModuleBalancingClient` 是一个基于 Go 开发的 Windows 客户端，用于与服务端协同完成模块文件的监听、解析、校验、下载、过期清理和客户端自升级。

程序会持续监听指定目录中的触发文件，当检测到新的 `DDD` 文件后，自动定位对应的 `DAT/AOD` 数据文件，向服务端请求模块清单，并将缺失或损坏的模块补齐到本地目录。

## 主要功能

- 监听 `Chkdir` 目录中新建的触发文件。
- 解析 `DAT/AOD` 文件中的模块清单，必要时回退到本地解析。
- 按文件大小和 CRC64 与服务端做完整性校验。
- 自动下载缺失模块，并支持断点重连。
- 接收服务端推送的过期模块删除指令。
- 周期性上报门店记录和心跳。
- 比对客户端 MD5，支持自升级。

## 目录结构

```text
.
├─ Modulebalancingclient.go   # 程序入口，协程调度与主流程
├─ api/                       # gRPC 调用、下载、解析、升级逻辑
├─ env/                       # 配置结构、CRC64、进度条、运行状态工具
├─ grpc/                      # protobuf 定义与生成代码
├─ logmanager/                # 按业务分类的日志管理
├─ conf/config.yaml           # 本地运行配置
├─ logs/                      # 运行日志输出目录
├─ temp/                      # 临时文件与升级文件
└─ bin/                       # 编译产物
```

## 核心流程

1. 程序启动后读取 `conf/config.yaml`，建立到 gRPC 服务端的连接。
2. 扫描 `Chkdir`，补处理上次退出时遗留的触发文件。
3. 监听 `Chkdir` 中新增文件，将 `*.ddd` 映射为对应的 `*.dat`。
4. 调用 `Module.Analyzing` 获取模块名列表，失败时回退到本地解析。
5. 对每个模块执行存在性检查和 CRC64 校验，不一致则重新下载。
6. 全部模块准备完成后，在 `Common` 目录下生成对应的 `.OK` 标记文件。
7. 后台协程持续处理过期删除、门店上报、超时文件清理和客户端升级。

## 配置说明

配置文件位于 `conf/config.yaml`：

```yaml
Setting:
  Maxretentiondays: 30
  Common: "E:/Client/Common"
  Chkdir: "E:/Client/Check"
  AODdir: ""

GRPCServices:
  Host: localhost
  Port: 9998
```

- `Common`：模块文件和 `.OK` 标记文件所在目录。
- `Chkdir`：监听的触发目录，新增 `DDD` 文件后开始处理。
- `AODdir`：原始 `DAT/AOD` 文件目录。
- `Maxretentiondays`：用于通知服务端清理过期模块。
- `Host` / `Port`：gRPC 服务端地址。

## 构建与运行

### 环境要求

- Go `1.24.1`
- Windows 环境

该项目使用了 `syscall` 和 `.exe` 升级流程，当前实现明显面向 Windows。

### 常用命令

```powershell
go mod tidy
go build -o bin/ModuleBalancingClient.exe .
go run .
go test ./...
go fmt ./...
```

如修改了 `grpc/ModuleBalancing.proto`，可重新生成代码：

```powershell
protoc --go_out=. --go-grpc_out=. grpc/ModuleBalancing.proto
```

## gRPC 服务依赖

客户端依赖以下服务端接口：

- `Module.Analyzing`
- `Module.IntegrityVerification`
- `Module.Push`
- `Module.ModuleReload`
- `Expirationpush.Expiration`
- `Storerecord.Updatestorerecord`
- `ClientCheck.MD5`
- `ClientCheck.Data`

如果服务端未启动或协议不匹配，客户端会在下载、上报、过期清理或升级阶段失败。

## 日志说明

日志按业务分目录输出在 `logs/` 下，主要包括：

- `logs/download`
- `logs/monitor`
- `logs/store`
- `logs/expiration`
- `logs/clean`

## 代码分析结论

这份代码的整体职责比较集中，主流程清晰，适合部署为常驻客户端服务。当前实现的重点在于稳定下载、断线重连和文件完整性校验，业务边界主要由 `grpc/ModuleBalancing.proto` 定义。需要注意的是，入口文件偏大，后续如果继续扩展，建议把监听、升级、过期处理拆分成独立包，降低 `main` 包复杂度。
