# zap — 按 Git 仓库管理的本地快速快照

`zap` 将完整 **restic CLI** 打进单个二进制，并在每个 Git 仓库下提供一套简化的快照工作流（由原 **rundo** 工具移植而来）。无需单独安装 `restic`。

所有经 `zap` 触发的 restic 调用**默认开启 `--fast` 快速模式**（详见 [README-FAST.zh-CN.md](README-FAST.zh-CN.md)）。需要完整安全保证时，可用 `zap restic --fast=false …` 关闭。

---

## 适用场景

| 适合 | 不适合 |
|------|--------|
| 本机开发仓库，单人串行保存快照 | 多用户共享机器、多人同时写同一仓库 |
| 工作区在 Git 仓库内 | 非 Git 目录（`zap` 依赖 `git rev-parse`） |
| 能接受 `--fast` 的安全权衡（见下文） | 需要生产级、密码学严格防护的远程备份 |

---

## 安装与编译

```bash
# 在仓库根目录
go build -o zap ./cmd/zap

# 或进入 cmd/zap
cd cmd/zap
go build -o zap
```

将生成的 `zap` 加入 `PATH`，或在项目里用绝对路径调用。

**依赖（部分子命令）：**

| 命令 | 外部工具 |
|------|----------|
| `diff` | `diff` |
| `restore`（整树） | `rsync` |
| `restore`（单路径） | `cp` |
| 所有命令 | `git` |

---

## 快速开始

在 **Git 仓库根目录** 内执行：

```bash
# 1. 保存当前工作区快照（首次 save 会自动 init，默认标签 manual）
zap save

# 2. 带自定义标签保存
zap save before-refactor

# 3. 查看最近快照
zap list

# 4. 与当前工作区对比（默认最新快照）
zap diff

# 5. 恢复到某一快照（整树）
zap restore latest

# 可选：显式初始化（首次 save 也会做同样的事）
zap init
```

---

## 数据存放位置

`zap init` 后，元数据与仓库位于 Git 目录下（由 `git rev-parse --git-path` 解析，通常为 `.git/zap-restic/`）：

```
.git/zap-restic/
├── repo/          # restic 仓库
├── password       # 自动生成的仓库密码（权限 0600）
└── excludes.txt   # 默认排除路径列表（可编辑）
```

快照使用 restic 的 `--host`、`--tag zap` 以及你提供的 **label 标签** 区分；`list` / `save` 等只操作当前 Git 根目录对应 host 的快照。

**默认排除目录**（写入 `excludes.txt`，可按需修改）：

`.git`、`node_modules`、`.venv`、`venv`、`__pycache__`、`.pytest_cache`、`.mypy_cache`、`.ruff_cache`、`.next`、`.turbo`、`dist`、`build`、`target`

`diff` / 整树 `restore` 还会额外跳过部分缓存目录，避免误改 `.git` 等。

---

## 命令说明

### `zap init`

在当前 Git 仓库下创建快照环境：生成密码、默认排除文件，并 `restic init`（若仓库已存在则跳过初始化）。

```bash
zap init
```

---

### `zap save [label] [--no-list]`

备份整个 Git 工作区根目录。

- **首次执行**时若尚未初始化，会自动执行与 `zap init` 相同的步骤（创建密码、排除文件与 restic 仓库），再继续备份
- 默认标签：`manual`
- 固定标签：`zap`（用于筛选本工具创建的快照）
- 使用 `--skip-if-unchanged`：内容未变时不产生新快照
- **默认列出**本次备份中新增与变更的文件（不含未修改项；目录变更见汇总行）；加 `--no-list` 可关闭

```bash
zap save
zap save wip
zap save "experiment-2024-06-02"
zap save --no-list          # 只显示汇总行，不逐条列出变更
zap save wip --no-list
```

---

### `zap list [-n N]`

列出最近快照，默认 **20** 条，按时间倒序。

```bash
zap list
zap list -n 50
zap list --limit=10
```

输出中的 ID 为短 ID（前 12 位），`diff` / `restore` 时可使用完整 ID 或 `latest`。

---

### `zap diff [snapshot]`

将指定快照还原到临时目录，用系统 `diff -ruN` 与**当前工作区**对比。

- 省略 snapshot 时等价于 `latest`
- 退出码与 `diff` 一致（有差异一般为 1）

```bash
zap diff
zap diff latest
zap diff a1b2c3d4e5f6
```

---

### `zap diff2 <snap_a> <snap_b>`

用内置 restic 对比两个快照之间的差异（restic 原生 diff）。

```bash
zap diff2 latest abc123def456
zap diff2 snap1 snap2
```

---

### `zap restore <snapshot> [paths...]`

**无 paths：** 用 `rsync` 将快照内容同步回工作区根目录（`--delete`，会删除工作区中快照里不存在的文件；`.git` 等目录被排除，不会被覆盖）。

**有 paths：** 只恢复给定相对路径；若快照中不存在该路径，则删除工作区中对应文件（恢复到“该快照下的状态”）。

```bash
# 整树恢复（危险：会改动大量文件，请先 save 或确认）
zap restore latest

# 只恢复部分文件/目录
zap restore latest src/main.go
zap restore abc123 pkg/utils/
```

路径必须为**相对路径**，且不能包含 `..`。

---

### `zap check`

对当前仓库运行 `restic check`，校验数据完整性。

```bash
zap check
```

---

### `zap restic <args...>`

透传任意 restic 子命令；同样默认 `--fast`。关闭快速模式示例：

```bash
zap restic --fast=false snapshots
zap restic --fast=false forget --keep-last 5
zap restic stats
```

仓库与密码由 `zap` 自动注入，一般无需手写 `-r` / `--password-file`；若需完全手动控制，请自行指定路径。

---

## 典型工作流示例

```bash
# 大改前先存一份
zap save before-big-change

# … 编辑代码 …

# 看看相对最新快照改了什么
zap diff

# 只把某个目录救回来
zap restore latest src/components/

# 对比两次保存之间 restic 层面的差异
zap list -n 5
zap diff2 <id1> <id2>
```

---

## 与 `--fast` 模式的关系

`zap` 内嵌的 restic 在每次调用前会将 `--fast` 设为默认开启，效果包括：

- KDF 结果本机缓存，热启动显著加快
- backup 默认不加仓库锁（适合单人串行）
- 与 `save` 使用的 `--skip-if-unchanged` 配合，未变更时可跳过新快照

**主要风险**（完整说明见 [README-FAST.zh-CN.md](README-FAST.zh-CN.md)）：

1. 缓存目录可能存有**明文 master key**，同用户进程可读即等价于能解密仓库  
2. 不宜多进程同时对同一仓库执行 backup / prune  
3. `--fast` 下 `init` 的新仓库使用较弱 KDF（仅影响 `zap init` 时新建的空仓库）

开发机本地快照、且只有你一人串行使用时，通常可接受；生产或多人环境请使用官方 restic 且**不要**开启 `--fast`。

---

## 故障排查

| 现象 | 处理 |
|------|------|
| `not inside a git repository` | 在 Git 仓库内执行，或先 `git init` |
| `not initialized. Run: zap init` | 其他命令需先 `zap init`；或先执行一次 `zap save`（会自动 init） |
| 快照列表为空 | 确认在正确仓库根目录，且曾 `zap save` 成功 |
| 恢复后文件不对 | 检查是否误用整树 `restore`；优先用带 paths 的部分恢复 |
| 怀疑仓库损坏 | `zap check`；必要时 `zap restic repair`（谨慎） |
| 想清空 KDF 缓存 | 删除 `~/Library/Caches/restic-fast-kdf/`（macOS）或 `~/.cache/restic-fast-kdf/`（Linux） |

调试 restic 各阶段耗时可设置：

```bash
RESTIC_PROFILE=1 zap save
```

---

## 帮助

```bash
zap help    # 或 zap -h / zap --help
```

---

## 相关文档

- [README-FAST.zh-CN.md](README-FAST.zh-CN.md) — `--fast` 实现细节、性能与安全说明
- [cmd/zap/main.go](cmd/zap/main.go) — 命令实现源码
