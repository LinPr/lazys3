# lazys3

[English](README.md) | 简体中文

一个像文件管理器一样浏览和操作 S3 兼容对象存储（AWS S3、阿里云 OSS、MinIO 等）的终端 UI —— 双栏传输、预签名 URL、版本管理，全程键盘操作。

![demo](docs/demo.gif)

## 功能特性

**浏览**

- 多 Profile：自动读取 `~/.aws/config` / `~/.aws/credentials` 中的全部 profile，支持自定义 `endpoint_url` 接入 S3 兼容服务（MinIO、阿里云 OSS、腾讯云 COS、Ceph 等）
- 自动选择寻址方式：AWS S3 与阿里云 OSS 使用 virtual-host 风格，其余自定义 endpoint 一律使用 path-style（OSS 不接受 path-style，因此做了特判）
- 使用 `enter`/`backspace`（或方向键）在 profile → bucket → 对象之间逐级导航，前缀（prefix）表现得像目录
- 内容预览（`p`）：浮动浮层显示高亮文件的前 256 KiB（S3 对象按 Range 拉取），可滚动，自动识别二进制文件和空文件
- 元数据浮层（`m`）：显示高亮条目的全部非空字段 —— S3 对象为完整 `HeadObject`（Content-Type、ETag、存储类型、用户元数据、SSE、校验和等），bucket 为地域/版本控制状态，本地条目为路径/权限/属主/时间戳/软链接目标，profile 为配置文件路径
- 每个列表都支持过滤（`/`）、按名称/大小/时间排序（`o`/`O`）和多选（`space`、`a`）

**传输**

- 双栏模式（`l`）：一侧本地文件系统、一侧 S3；`tab` 切换焦点，文件操作作用于当前聚焦的一栏
- 上传（本地栏 `u`）与下载（远端栏 `d`），文件夹通过同步引擎递归传输
- 目录同步（`s`）：本地 ⇄ S3、S3 ⇄ S3；双栏模式下自动预填两侧路径
- 实时传输浮层（`t`），带进度条、最新在前；`x` 取消正在运行的传输
- 路径直达（`g`）：直接跳转到 `s3://bucket/prefix/` 或本地目录，无需逐级导航

**管理**

- 创建 bucket 与本地目录（`B`）、重命名（`r`）、拷贝（`c`）
- 递归删除（`D`）：S3 文件夹（前缀）与本地目录，以及删除空 bucket —— 全部有确认弹窗把关
- 浮动确认弹窗带 Yes/No 按钮：`tab`/方向键切换高亮，`enter` 执行高亮按钮（默认 Yes），`y`/`n`/`esc` 随时可用

**版本与分享**

- 预签名分享 URL（`Y`），有效期可配（1s..168h，默认 1h），生成后直接复制到剪贴板
- 对象版本（对象列表按 `v`）：下载、恢复或删除指定版本
- 切换 bucket 版本控制（bucket 列表按 `v`）：Enabled ⇄ Suspended
- 复制路径（`y`）：bucket/对象的 `s3://` URI，或本地条目的绝对路径

**体验**

- 应用内可滚动的帮助浮层（`?`），内容与下方按键表一致
- 单行状态栏：当前 profile、聚焦面板、选中数量、传输进度（传输进行时显示聚合进度条和各方向批次计数，空闲时显示累计 `↑`/`↓` 完成数与 `✗` 失败数）、最近一条提示（如 `o`/`O` 切换后的排序状态）或错误
- 通过 `config.yaml` 自定义主题颜色，可选 Nerd Font 文件图标
- 配置文件损坏不会导致崩溃 —— 非法值自动回退到默认值并记录日志

## 按键说明

在 lazys3 中按 `?` 即可查看这份按键表的可滚动浮层版本。

### 全局

| 按键 | 作用 |
|---|---|
| `q` | 退出 lazys3 |
| `ctrl+c` | 强制退出 |
| `?` | 开关帮助浮层 |
| `t` | 开关实时传输浮层（最新在前；`enter`：sync 逐文件明细，`←`/`→` 横向滚动宽表格） |
| `x` | 取消最近一个正在运行的传输（传输浮层内：取消高亮的那个） |
| `l` | 开关双栏布局（本地 ⇄ 远端，需要 ≥80 列） |
| `tab` | 在远端栏和本地栏之间切换焦点（双栏模式） |
| `p` | 预览文件内容（浮动浮层，前 256 KiB） |
| `m` | 对象/文件元数据（浮动浮层；也支持 bucket 和 profile） |
| `enter` / `→` | 打开选中项（profile → bucket → 对象） |
| `backspace` / `←` | 返回上一级 |
| `↑`/`k`、`↓`/`j` | 移动列表光标（也用于滚动 `?`/`t`/`v`/`p`/`m` 浮层） |

### 远端栏（S3）

| 按键 | 作用 |
|---|---|
| `d` | 下载选中对象；双栏模式下也支持文件夹，下载到本地栏当前目录 |
| `u` | 上传本地文件到当前前缀（仅单栏模式；双栏模式会提示按 `tab` —— 上传需在本地栏聚焦时执行） |
| `D` | 删除选中对象；文件夹递归删除（不可恢复）/ 删除空 bucket（bucket 列表） |
| `r` | 重命名选中对象（copy + delete 实现） |
| `c` | 拷贝选中对象到 `s3://bucket/key`（双栏模式：拷贝到本地栏） |
| `B` | 创建 bucket（bucket 列表；对象列表中只给出提示） |
| `s` | 目录同步（本地 ⇄ s3、s3 ⇄ s3；双栏模式自动预填两侧） |
| `y` | 复制高亮 bucket/对象的 `s3://` URI 到剪贴板 |
| `Y` | 生成预签名分享 URL（仅对象文件） |
| `v` | 对象版本（对象列表）/ 切换 bucket 版本控制 Enabled ⇄ Suspended（bucket 列表） |
| `g` | 路径直达：`s3://bucket/prefix/` 可切换 bucket，`/path` 从 bucket 根开始，`rel/path` 相对当前前缀 |

### 本地栏

| 按键 | 作用 |
|---|---|
| `u` | 上传选中项到远端 bucket/前缀（文件夹递归同步） |
| `c` | 拷贝选中项到远端栏（等同 `u`） |
| `d` | 提示按 `tab`（下载需在远端栏聚焦时执行） |
| `D` | 删除选中项（不可恢复，无回收站；目录递归删除） |
| `r` | 重命名高亮条目（同目录内） |
| `B` | 创建目录 |
| `s` | 目录同步：本地栏 → 远端栏（自动预填，可编辑） |
| `y` | 复制高亮条目的绝对路径到剪贴板 |
| `g` | 目录直达（绝对路径、`~` 或相对当前目录） |

### 选择与过滤

| 按键 | 作用 |
|---|---|
| `space` | 切换高亮条目的选中状态（✔ 标记） |
| `a` | 反选（全选 ↔ 全不选） |
| `/` | 过滤当前列表（`enter` 应用，`esc` 清除） |
| `o` | 循环切换排序字段（名称 → 大小 → 时间） |
| `O` | 反转排序方向 |

### 浮层

| 按键 | 作用 |
|---|---|
| `pgup` / `pgdn` | 翻页 |
| `g` / `G` | 跳到顶部 / 底部（帮助、传输、预览、元数据浮层） |
| `esc` | 关闭浮层（列表：清除过滤；弹窗：取消） |

## 安装

需要 Go 1.25+。

```sh
go install github.com/LinPr/lazys3@latest
```

或从源码构建：

```sh
git clone https://github.com/LinPr/lazys3.git
cd lazys3
go build .          # 或：task build
```

## 快速上手

lazys3 读取标准的 AWS 共享配置。在 `~/.aws/config` 中为每个存储账号建一个 profile：

```ini
# ~/.aws/config
[default]
region = us-east-1

[profile oss]
region = cn-hangzhou
endpoint_url = https://oss-cn-hangzhou.aliyuncs.com
```

密钥放在 `~/.aws/credentials`：

```ini
# ~/.aws/credentials
[default]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY

[oss]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY
```

`default` profile 指向 AWS S3；带 `endpoint_url` 的 profile 指向对应的 S3 兼容服务（MinIO、阿里云 OSS 等）。path-style 与 virtual-host 寻址会按 endpoint 自动选择。

然后启动：

```sh
lazys3
```

选择一个 profile，按 `enter` 列出 bucket，随时按 `?` 查看完整按键表。

## 配置

lazys3 读取 `$XDG_CONFIG_HOME/lazys3/config.yaml`（默认 `~/.config/lazys3/config.yaml`）。首次运行会写入一份带注释的模板，方便发现所有配置项。所有键都是可选的；非法值会回退到内置默认值并记录日志。

```yaml
# lazys3 configuration.
# All keys are optional; the commented values show the built-in defaults.

theme:
  # Colors are hex strings: "#rgb" or "#rrggbb".
  # focused_border: "#20e71c"    # border of the focused pane
  # unfocused_border: "#555555"  # border of the unfocused pane (dual-pane mode)
  # title_fg: "#e39f00"          # status-bar profile chip foreground
  # title_bg: "#444745"          # status-bar profile chip background
  # status_error_fg: "#ffffff"   # status-bar error text
  # selected_fg: ""              # highlighted list row foreground

ui:
  # nerd_font: false             # render Nerd Font file icons (needs a patched font)
  # default_sort: "name"         # initial sort field: name | size | time
  # sort_desc: false             # sort descending by default

local:
  # start_dir: ""                # local pane start directory, "~" ok (default: process cwd)
```

命令行参数：

- `--config <path>` —— 使用指定的 lazys3 配置文件替代默认位置（`lazys3 --config ./lazys3.yaml`）；文件必须存在、可读且是合法 YAML。
- `--aws-config <path>` —— AWS 共享 config 文件（`lazys3 --aws-config ~/work/aws-config`）；优先级：flag > `AWS_CONFIG_FILE` > `~/.aws/config`。
- `--aws-credentials <path>` —— AWS 共享 credentials 文件（`lazys3 --aws-credentials ~/work/aws-credentials`）；优先级：flag > `AWS_SHARED_CREDENTIALS_FILE` > `~/.aws/credentials`。

说明：

- 旧版本使用 `config.toml`，现已不再读取 —— 请重命名为 `config.yaml` 并把内容转换为 YAML 语法（stderr 会打印提示，且不会写模板覆盖它）。
- `local.start_dir` 支持 `~` 和相对路径（相对启动目录解析）；必须是已存在的目录，否则会被忽略。
- 旧版本的 `ui.transfer_panel_height` 已废弃且被忽略 —— 底部传输面板已被全屏传输浮层（`t`）取代。旧配置文件仍可正常加载。

## 许可证

MIT —— 见 [LICENSE](LICENSE)。

---

参与开发（构建、测试、演示 GIF 录制，英文）：[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)
