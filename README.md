# LegalScout

LegalScout 是面向律师和律师助理的**批量网页核查执行器**。它将名单、任务状态和截图严格隔离到各个项目中，自动完成证券合规核查的重复操作；交付物是带时间水印、按对象整理的页面 PNG 截图。

它不是法律问答、风险评分、案件管理或 HTML/PDF 报告工具。

## 安装与构建

需要 Go 1.25+。LegalScout 不打包 Chrome；可使用本机 Chrome/Chromium 或可选的 Browserless。

```bash
go install github.com/Duan-JM/LegalScout/cmd/legalscout@latest
# 在源码目录开发：
go build -o legalscout ./cmd/legalscout
```

macOS 上也可把当前仓库注册为仅限本机的 Homebrew tap，再从本地 `main` 分支当前已提交的 HEAD 构建并管理开发版：

```bash
make brew-install-dev
# 提交新代码后更新已安装的开发版：
make brew-reinstall-dev
# 卸载开发版：
make brew-uninstall-dev
```

开发版的 Homebrew Formula 安装名为 `legalscout-dev`，命令仍为 `legalscout`。安装和更新命令会同步本地 tap，但构建只包含本地 `main` 分支已提交的内容，不包含其它分支或工作区中未提交的改动。若以后安装同样提供 `legalscout` 命令的稳定版，需要先卸载开发版；开发版卸载命令也会移除这个本地 tap。

先检查运行环境：

```bash
legalscout doctor
```

## 主路径

```bash
legalscout new "恒星科技 IPO" --input ./名单.xlsx --checklist securities
legalscout start 恒星科技-ipo       # 写入后台队列后立即返回
legalscout status                  # 所有项目
legalscout review 恒星科技-ipo      # 可见 Chrome 中完成验证码/确认
legalscout open 恒星科技-ipo        # 打开可交付截图目录
```

无参数运行 `legalscout` 可打开 Bubble Tea 项目面板。`start`、`status`、`review`、`retry`、`open` 和 `archive` 接受 slug、短 ID、唯一项目名称或唯一前缀；在项目目录中可省略参数。全局目录中省略项目会给出明确错误，而不会猜测当前项目。

其它命令：

```text
legalscout retry [项目]    # 仅重置 retryable_error；fatal_error 必须修复来源配置
legalscout archive [项目]  # 移至 ~/LegalScout/_archive，截图不删除
legalscout doctor          # 浏览器、Remote CDP、工作区、SQLite、来源预检
```

## 工作区与目录

默认工作区为 `~/LegalScout`。用 `LEGALSCOUT_WORKSPACE` 覆盖，例如：

```bash
export LEGALSCOUT_WORKSPACE="$HOME/案件/LegalScout"
```

每个项目是独立目录，原始输入会复制留档：

```text
~/LegalScout/
  恒星科技-ipo/
    名单.xlsx
    截图/
      001-张三/
        证监会政府信息公开-无记录-20260813-093000.png
        上交所监管信息-发现记录-20260813-093125.png
    _legalscout/
      state.db
      logs/
      diagnostics/            # 异常页面截图，绝不混入交付目录
```

Excel 优先读取名为 `名称`、`姓名`、`主体` 或 `name` 的列，否则读取首列；`.txt`（每行一个名称）也兼容。空值会过滤，去重稳定保留首次出现顺序。`securities` 清单为每个对象创建四项任务：证监会政府信息公开、上交所监管信息、深交所监管信息和证券期货市场失信记录。

## 执行、恢复与状态

全局队列使用纯 Go SQLite 驱动，不依赖 CGO。它使用项目锁、任务租约和过期租约回收；每轮只取一个项目任务并重新排队，以免大项目饿死小项目。`LEGALSCOUT_MAX_CONCURRENCY` 可设置为 1–8，默认保守值为 1，是整个工作区共享的浏览器并发上限。

单项状态为 `pending`、`running`、`not_found`、`found`、`needs_review`、`retryable_error`、`fatal_error`。只有页面的明确“无记录”规则能产生 `not_found`；网络问题、浏览器断连、读取失败、验证码和来源页面结构变化绝不会伪装成“无记录”。

成功截图全页 PNG 均含“对象 | 来源 | 本地毫秒时间”水印，文件名含来源、状态和 `YYYYMMDD-HHMMSS`。写入使用同目录临时文件、fsync、close 和原子 rename；同秒重复确认会使用稳定的 `-2`、`-3` 后缀，不会覆盖先前交付物。确认结果不会在常规重试中再次执行或覆盖；内部 SQLite 记录后续人工重新确认时的截图替换关系。二进制嵌入 Noto Sans SC（SIL OFL 1.1，许可证见 `internal/capture/assets/OFL.txt`）作为中文后备；检测到本机 CJK TTF 时优先使用它渲染中文水印。

证券期货市场失信记录可能出现验证码。自动任务会进入 `needs_review`，**不会绕过或破解验证码**。运行 `review` 后，LegalScout 启动可见 Chrome；用户自行完成验证并在终端按 Enter，程序才读取结果、截图和归档。**Remote CDP/Browserless 不支持人工 review**，因为它不能保证验证码页面在操作者的本机可见；请临时取消 `BROWSERLESS_URL` 并使用 Local Chrome。

## 本机 Chrome 与 Browserless

默认自动检测本机 Google Chrome/Chromium。若使用 Remote CDP/Browserless，设置：

```bash
export BROWSERLESS_URL='ws://localhost:3000?token=legalscout-local'
legalscout doctor
```

仓库保留 `docker-compose.yml` 仅作为可选 Browserless 服务：

```bash
docker compose up -d
export BROWSERLESS_URL='ws://localhost:3000?token=legalscout-local'
```

`doctor` 会以中文报告 Chrome 路径与版本、Remote CDP、工作区权限、SQLite 可写性以及四个来源的预检。预检和实际运行才会访问网站；常规单元测试不依赖实时网站。

Remote CDP 可使用完整的 Browserless `ws://` 或 `wss://` URL（包括 token 查询参数）；诊断输出会脱敏 token、userinfo 和 API key。HTTP(S) CDP 地址则通过标准 `/json/version` discovery 连接。

## 开发与验证

```bash
go test ./...
go vet ./...
go build ./cmd/legalscout
```

GitHub Actions 在 macOS、Windows 和 Linux 上运行测试与构建。项目未使用 Python、Poetry、Playwright 或 CGO。

## 产品边界与预览验收

代码实现了多项目隔离、队列恢复、四来源适配契约和本机/Remote CDP 执行路径；网站持续变动、验证码和真实业务页面必须在预览阶段进行实际验证。发布前仍需满足 issue #7 的真实用户门槛：至少 3 位律师/律师助理，各完成 2 个项目、每项目至少 20 个对象，并验证相较人工重复操作节省至少 50% 时间。该真人验证不是代码或自动化测试可以伪造完成的项目。
