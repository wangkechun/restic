# restic 快速模式（`--fast`）说明

本仓库在官方 restic 基础上增加了 **`--fast` 本地快速模式**，面向 rundo 等**单人、本地、串行**备份场景，用可控的安全代价换取明显更低的启动延迟。

版本标识：`0.18.1-dev-fast`

---

## 修改了什么

| 改动 | 作用 | 节省耗时（典型） |
|------|------|------------------|
| **`--fast` 全局开关** | 启用下列所有快速路径 | — |
| **KDF 结果缓存** | 首次 scrypt 成功后，将 master key 缓存到本机，后续跳过 ~500ms 密钥派生 | 第二次起约 **500ms → <1ms** |
| **backup 跳过仓库锁** | 不再创建 lock 文件，不再 sleep 200ms 等待锁一致性检查 | 约 **220ms** |
| **锁等待归零** | `waitBeforeLockCheck = 0` | 已含在上项 |
| **`--skip-if-unchanged`**（rundo 侧） | 内容未变时不写新 snapshot | 约 5–15ms |
| **新仓库弱 KDF**（仅 `init`） | 新建 key 使用 N=128（官方约 N=32768） | 仅影响新建仓库 |

### 涉及的主要文件

```
internal/restic/fast_mode.go       # 锁等待归零
internal/repository/fast_mode.go   # 快速模式入口、新 key 弱 KDF
internal/repository/keycache.go    # KDF 缓存读写
internal/repository/key.go         # OpenKey 读缓存
internal/global/global.go          # --fast 全局 flag
cmd/restic/lock.go                 # backup 在 fast/no-lock 时跳过加锁
```

### 性能参考（单文件、内容未变、KDF 缓存已命中）

| 方式 | 耗时 |
|------|------|
| 官方 restic backup | ~770ms |
| `--fast` 首次（冷启动，仍跑 scrypt） | ~550ms |
| **`--fast` 热启动（KDF 缓存命中）** | **~15–35ms** |
| `git status`（对比参考） | ~20ms |

可用 `RESTIC_PROFILE=1` 查看各阶段耗时（开发调试用）。

---

## 如何使用

### 编译

```bash
cd cmd/restic
go build -o restic-fast
```

安装到 PATH，或通过环境变量指定：

```bash
export RUNDORESTIC=/path/to/restic-fast   # rundo 会读取此变量
```

### 命令行

```bash
# 方式一：flag
restic-fast --fast -r /path/to/repo --password-file /path/to/password backup /data

# 方式二：环境变量
export RESTIC_FAST=1
restic-fast -r /path/to/repo backup /data
```

### 与 rundo 配合

rundo 默认调用 `restic-fast` 并附带 `--fast`、`--skip-if-unchanged`。  
确保 `restic-fast` 在 PATH 中，或设置 `RUNDORESTIC` 指向二进制路径。

### KDF 缓存位置

```
~/Library/Caches/restic-fast-kdf/   # macOS
~/.cache/restic-fast-kdf/           # Linux（XDG 规范下）
```

换密码、换仓库 key、或怀疑缓存损坏时，可删除对应目录，下次会重新跑 scrypt。

---

## 安全性说明

> ** `--fast` 仅适合：本机单人、串行 backup、不与其他 restic 写操作并发。**

### 1. KDF 缓存：仓库密钥明文落盘（主要风险）

正常 restic 每次启动都需 **密码 + scrypt**，master key 不写入磁盘。

`--fast` 首次成功后，会在缓存目录写入 JSON，其中包含 **解密仓库所需的 master key（明文）**，仅靠文件权限 `0600` 保护，**不再经过密码加密**。

| 风险 | 说明 |
|------|------|
| 同用户其他进程 | 读取缓存即可解密整个仓库，无需知道密码 |
| root / 全机备份 / 云同步缓存目录 | 缓存泄露 ≈ 仓库泄露 |
| 恶意软件 | 比暴力破解 scrypt 容易得多 |

**请勿**将 `restic-fast-kdf` 目录同步到云盘、纳入公开备份、或在多用户机器上使用。

### 2. backup 不加锁

官方 restic 通过 lock 文件保证**同一时刻只有一个写者**。  
`--fast` 下 backup **不创建 lock**，多进程可同时写仓库。

### 3. 新仓库弱 KDF（仅 `init`）

`--fast` 下 `restic init` 新建 key 使用 `N=128, r=1, p=1`，密码暴力破解远比官方容易。  
**已有仓库**的 key 参数不变（例如 N=32768）；只有新建仓库受影响。

### 4. 并发写仓库的可能后果

| 场景 | 可能结果 |
|------|----------|
| 两个 backup 同时跑 | 通常各产生一个 snapshot；极端情况下同时写同一 pack 文件可能导致 pack 损坏 |
| backup + prune/forget 同时 | prune 可能删除 backup 仍引用的 pack → **snapshot 不可 restore** |
| backup + repair/rebuild-index | 索引与数据不一致，`restic check` 失败 |
| 单进程串行 save（rundo 典型用法） | 风险很低 |

---

## 建议使用场景

| 适合 | 不适合 |
|------|--------|
| 本机开发机，只有你一个人 | 多用户 / 共享服务器 |
| rundo 手动或 hook 串行 save | CI 并行 backup 同一仓库 |
| 本地仓库（`.git/rundo-restic/repo`） | 远程 SFTP/S3 多人协作仓库 |
| 能接受缓存目录在本机受控 | 需要严格密码学防护的生产备份 |

---

## 与官方 restic 的差异汇总

```
官方 restic backup:
  scrypt(~500ms) → 加锁(~220ms) → LoadIndex → 扫描/备份 → 写 snapshot

restic-fast --fast backup（热启动）:
  读 KDF 缓存(~1ms) → 跳过锁 → LoadIndex → 扫描/备份 → (--skip-if-unchanged 可跳过 snapshot)
```

需要完整安全保证时，请使用官方 restic，**不要**加 `--fast`。

---

## 调试

```bash
RESTIC_PROFILE=1 restic-fast --fast backup ...
```

stderr 会输出 `RESTIC_PROFILE` 各阶段耗时，便于分析瓶颈。
