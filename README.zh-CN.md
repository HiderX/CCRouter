<p align="center"><img src="designs/icon/ccrouter.png" width="128" alt="CCRouter 图标，一朵彼岸花"></p>

# CCRouter

[English](README.md) | **简体中文**

同时使用多个 Claude Code 账户，又不会把它们混在一起。CCRouter 把每个对话固定在创建它的账户上，并在原生 macOS 菜单栏面板里显示所有账户的用量和额度。在终端里，`ccrouter` 的用法和 `claude` 一样，只是会先让你选择账户。

没有反向代理。CCRouter 只负责选好账户的配置目录，然后把终端交给官方的 `claude`。登录、账户资料和额度都来自官方 CLI。CCRouter 自己只发一种请求：不带任何账户信息的出口 IP 探测。

<p align="center">
  <img src="designs/native-b2-preview.png" width="280" alt="CCRouter 菜单栏面板，浅色外观">
  &nbsp;&nbsp;
  <img src="designs/native-b2-dark-preview.png" width="280" alt="CCRouter 菜单栏面板，深色外观">
</p>
<p align="center"><sub>使用演示数据的离屏渲染。实际在屏幕上，面板底下是系统弹出面板的材质。</sub></p>

## 为什么用 CCRouter

- **不经手任何请求**。CCRouter 不转发模型流量，也看不到你的提示词。它只准备好运行环境，然后通过 `exec` 把自己替换成 `claude`。请求、流式输出和报错都和官方 CLI 完全一致。
- **账户相关的请求都来自 Claude Code**。CCRouter 没有调用 Anthropic API 的代码，也从不读取 OAuth token。额度由 Claude Code 自己查询（`claude -p /usage`，一条不调用模型的本地命令）。CCRouter 唯一发往 Anthropic 域名的请求是匿名的出口 IP 探测，详见[网络请求](#网络请求)。
- **对话始终留在原账户**。每个对话 ID 只绑定一个账户。恢复时默认用这个账户，即使你之后改了默认账户也一样，因此不会丢失提示词缓存，也不会无意中把一段历史拆散到不同账户里。换到别的账户始终需要你主动选择。
- **几乎不占资源**。每个对话不额外留下进程，不轮询，不用 Electron 或 WebView。见下方[实测数据](#资源占用)。
- **用量和额度一目了然**。今日、近 7 天或全部的 token 用量，分账户和合计，附带缓存命中率和最近一小时趋势。每个账户的 5 小时和每周额度窗口，以及当前探测到的出口 IP。
- **从设计上保护隐私**。本地数据库不保存 token、提示词或对话正文。恢复列表里的对话标题在打开列表时从 Claude 自己的对话记录读取，从不保存。
- **防止常见失误**。账户从未登录过时，CCRouter 拒绝启动 Claude；会移除继承来的 API key 和 token 环境变量，避免它们悄悄顶替所选账户。

## 资源占用

| | 实测 |
| --- | --- |
| 启动器交给 `claude` 之前的耗时 | 约 18 ms（30 次的中位数） |
| 每个对话额外留下的进程 | 0 个。启动器通过 `exec` 直接变成 `claude`。 |
| 菜单栏应用的内存 | 约 48 MB 物理内存（活动监视器显示的口径） |
| 菜单栏应用的 CPU | 面板关闭时约 0.01%；Claude 会话持续写入对话记录时约 0.05% |
| 磁盘 | 应用包 31 MB（启动器 8 MB，菜单栏应用 23 MB） |
| 后台工作 | 所有账户共用一个 FSEvents 事件流；对话记录按字节偏移增量读取；面板关闭时不重绘 |
| 额度刷新 | 只在打开面板、且读数已超过 5 分钟时进行：每个账户运行一次 `claude -p /usage`，约 2 秒，不调用模型、不消耗 token |

测量环境：Apple Silicon MacBook Pro（M1 Pro），macOS 15.7，两个账户，已有几百 MB 的对话记录。方法和细节见 [docs/verification.md](docs/verification.md)。

## 系统要求

- 需要 macOS 13 或更高版本。预编译版本适用于 Apple Silicon。已在 macOS 15.7 上测试。
- 已安装 [Claude Code](https://code.claude.com/docs/en/setup)，并使用通过 `claude auth login` 登录的 Claude 订阅账户（Pro、Max 或 Team）。
- 构建需要 Go 1.27.3 或更高版本，以及 Xcode 或 Xcode Command Line Tools。

## 安装

### 下载

从 [Releases](https://github.com/HiderX/CCRouter/releases) 下载 `CCRouter-<版本>-macos-arm64.zip`，解压后把 `CCRouter.app` 移到「应用程序」文件夹。预编译版本适用于 Apple Silicon。

应用没有经过公证（公证需要付费的 Apple Developer ID），所以第一次打开时会被 macOS 拦下。先清除一次下载隔离标记，再打开：

```sh
xattr -dr com.apple.quarantine /Applications/CCRouter.app
open /Applications/CCRouter.app
```

如果想先校验下载的文件，可以用发布页里的 `.sha256` 文件比对：`shasum -a 256 -c CCRouter-<版本>-macos-arm64.zip.sha256`。

把启动器放到 `PATH` 上，就可以像 `claude` 一样使用 `ccrouter`：

```sh
ln -s /Applications/CCRouter.app/Contents/MacOS/ccrouter ~/.local/bin/ccrouter
```

任何在 `PATH` 里的目录都可以。

### 从源码构建

```sh
git clone https://github.com/HiderX/CCRouter.git
cd CCRouter
make app
open dist/CCRouter.app
ln -s "$PWD/dist/CCRouter.app/Contents/MacOS/ccrouter" ~/.local/bin/ccrouter
```

本地构建使用 ad-hoc 签名，在构建它的那台 Mac 上使用足够了；之后重新 `make app`，链接会自动指向新版本。`make release` 会生成发布页上的 zip 和校验文件。

## 快速开始

1. **添加账户**。在菜单栏面板点「添加账户」，起个名字，然后在终端里完成 Claude 官方登录。通过这种方式添加的账户各自有独立的配置目录。如果想登记 `~/.claude` 里已有的登录，在齿轮菜单里选「导入当前 Claude 账户」。
2. **开始对话**。在项目目录里运行 `ccrouter`，选择账户。默认账户已经预选，直接回车即可。之后 Claude 照常启动。
3. **稍后继续**。在任意目录运行 `ccrouter --resume`，从所有账户的对话中挑一个，也可以输入关键字筛选。接着 CCRouter 会问用哪个账户继续，上次使用的账户已预选，直接回车就沿用。然后进入该对话的项目目录并恢复。

## 终端用法

```sh
ccrouter                          # 在当前目录开新对话，先选择账户
ccrouter --resume                 # 从所有账户的对话中选择并恢复
ccrouter --resume 周报            # 带搜索词打开列表
ccrouter -c                       # 继续当前目录最近的对话
ccrouter --account 工作           # 跳过选择，直接用这个账户
ccrouter -c --switch-account 备用 # 换另一个账户接着做（缓存从头建立）
ccrouter --model opus "修复登录"  # 其他参数原样交给 claude
```

账户列表会显示每个账户的邮箱，并标出默认账户和未登录的账户，可以用方向键或直接按数字选择。对话列表显示每个对话的标题、项目、最近活动时间和账户。标题是 Claude 自动生成的；没有标题时显示第一句提问。

恢复对话时（`--resume`，带不带 ID 都一样，以及 `-c`），还会问用哪个账户继续。每个账户都显示当前额度，对话上次使用的账户标为「上次使用」并预选。选别的账户就把对话换过去，提示缓存从头建立。开启自动调度后，如果对话的账户已达阈值，预选的是标为「自动调度推荐」的账户。带 `--account` 或 `--switch-account` 时不再询问。

开启自动调度后（见[工作原理](#工作原理)里的「自动调度」），新对话的账户列表会预选调度策略选中的账户，并标为「自动调度推荐」，直接回车即可，启动时会显示原因。每个账户都显示当前额度，也可以改选其他账户。在脚本和管道中由策略直接选择，不再询问。`--account` 仍然可以直接指定。

列表只在交互式终端里出现。在脚本、管道和 `-p`（打印）模式下，新对话直接使用默认账户（自动调度时由策略选择），`--resume` 需要完整的对话 ID。`mcp`、`doctor`、`update` 等 Claude 自己的管理命令不经过 CCRouter，请直接用 `claude` 运行。

其他命令：

| 命令 | 作用 |
| --- | --- |
| `ccrouter gui` | 打开菜单栏应用 |
| `ccrouter run [参数]` | 不询问，直接用默认账户、调度选出的账户或 `--account` 指定的账户启动 |
| `ccrouter account list` | 列出账户（`*` 表示默认） |
| `ccrouter account add 名称` | 新建使用独立配置目录的账户并登录 |
| `ccrouter account import [--name 名称] [--profile 目录]` | 登记已有的配置目录，默认是 `~/.claude` |
| `ccrouter account use 名称` | 设置新对话的默认账户 |
| `ccrouter account login 名称` | 重新登录 |
| `ccrouter account rename 名称 新名称` | 只修改显示名称 |
| `ccrouter schedule` | 查看调度模式、各账户阈值和下一个账户 |
| `ccrouter schedule manual` 或 `auto [最优\|轮询\|5h]` | 切换手动或自动调度，以及自动策略 |
| `ccrouter schedule threshold 名称 百分比` | 设置账户的阈值（50–100，步长 5） |
| `ccrouter schedule include\|exclude 名称` | 让账户参与或不参与自动调度 |
| `ccrouter schedule resume on\|off` | 续聊达到阈值时是否自动换号 |
| `ccrouter sessions` | 列出对话及其账户 |
| `ccrouter stats [--account 名称] [--days 1\|7\|0]` | 以 JSON 输出 token 统计（`--days 0` 表示全部） |
| `ccrouter quota` | 所有账户的额度窗口 |
| `ccrouter ip` | 探测到的出口 IP 和所在国家/地区 |
| `ccrouter scan` | 立即索引对话记录 |
| `ccrouter demo` | 用演示数据预览面板，不读取任何凭据 |

## 菜单栏面板

- **用量**：总 tokens、请求数和输入缓存命中率，最近一小时的趋势，以及缓存读取、缓存写入、未缓存输入、输出四项明细。点「今日」可在今日、近 7 天和全部之间切换。
- **账户**：点击账户的任意位置，展开它的邮箱和额度条，每条额度下方显示距离重置还有多久。在展开的账户上点「设为默认」，它就成为新对话的默认账户；✓ 标出当前的默认账户。自动调度时，✓ 换成蓝色箭头和「下一个」，标出新对话会用的账户；已达阈值、未登录或不参与调度的账户也会标出来，展开后额度条上的竖线就是这个账户的阈值。
- **出口 IP**：流量出口的地址和国家/地区，点击即可复制。
- **账户调度**：有多个账户时，原来的「续聊沿用原账户」这一行变成「账户调度」，点开后在面板里切换手动或自动。手动和以前一样：新对话用默认账户，续聊沿用原账户。自动时选择一种策略，每种策略右边显示它此刻会选哪个账户；下面设置哪些账户参与调度、每个账户的阈值，以及续聊达到阈值时是否自动换号，最后一句说明下一个对话为什么用这个账户。
- **最近对话**：在面板里原地展开最近的对话。点对话就在终端里恢复，用它原来的账户；自动调度要给它换号时，按调度换，行尾的账户名会显示「原账户 → 新账户」。点行尾的账户名，可以选另一个已登录的账户接着做，或者仍用原账户。「新对话…」选一个项目目录开新对话。
- 账户调度和最近对话展开时，用量区会暂时收起；展开其中一个会收起其他展开的部分，让面板不超出屏幕。
- **齿轮菜单**：导入 `~/.claude`、重命名账户、重新登录、刷新账户资料，或打开数据目录。

## 工作原理

- **每个账户一个配置目录**。这是 [Claude Code 文档](https://code.claude.com/docs/en/authentication)中同时保持多个账户登录的方法。Claude 按目录区分钥匙串条目，所以各账户的登录互不覆盖。导入的 `~/.claude` 保留它原来的钥匙串条目。
- **对话只存一份**。每个添加账户的 `projects`、`file-history` 和 `session-env` 都链接到 `~/.claude` 里的同名目录，所以任何账户都能直接恢复任何对话；登录凭据、设置和 `.claude.json` 仍按账户分开。账户第一次启动时，它之前单独记录的对话会以重命名的方式并入 `~/.claude`，不复制数据。如果同名文件内容不同，就保留原样，这个目录暂不共享；如果该账户还有 Claude 在运行，则等它退出后再合并。
- **启动前先绑定**。新对话事先分配好 ID（`--session-id`），并与账户一起记录；恢复时按 ID 查找。绑定只会改到实际运行这个对话的账户，见下一条；两个启动器同时运行也不会改乱。
- **主动换号**。`--switch-account`（配合 `--resume` 或 `-c`）只把对话的绑定改到另一个账户，不读写任何对话文件。对话 ID 不变，之后恢复就用新账户，提示缓存也在那边。换号后的第一次请求要完整重建提示缓存。在 Claude 里用 `/resume` 打开其他账户的对话时，绑定也会跟到当前账户。换号前的用量仍计在原账户，之后的用量计入新账户。
- **自动调度**。只在启动时决定，依据 Claude 已缓存的额度读数，启动不会为此等待刷新；已经过了重置时间的窗口按清零计算。自动调度开启时，菜单栏每 10 分钟在后台刷新一次额度。每个账户有自己的阈值（默认 95%），5 小时或本周任一用到阈值就算额度不足，所有策略都跳过它：
  - **5h/7d 最优**：先用快过期的额度。周额度才是要花掉的额度，5 小时窗口只限制花的速度，所以分数以周额度为主：本周余量按剩余天数平摊，再和匀速用完一周的速度比。高于匀速说明额度会在重置时作废，这个账户先用；低于匀速说明它用得太快，往后放。5 小时窗口最多让分数翻一倍：进行中的窗口离重置越近，加得越多；还没开始的窗口不会作废，不加分；余量不足一半时按比例减少；距离重置不到 30 分钟的窗口来不及用，按已重置计算。
  - **轮询**：按账户顺序，从上一次分配的账户之后找下一个可用的账户。
  - **5h 最大利用**：只看 5 小时窗口，先用最早重置、还有余量的账户；都没有进行中的窗口时，选余量最多的。
  
  续聊只在开启「续聊达到阈值时自动换号」、原账户参与调度并且已达阈值时，才换到策略选出的另一个账户。`--account` 和 `--switch-account` 总是优先。没有可用账户时，新对话使用默认账户。
- **干净的环境**。启动器会移除 `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN`、`CLAUDE_CODE_OAUTH_TOKEN` 等变量，并拒绝 `apiKeyHelper` 这类会顶替账户的设置。网络和代理设置保持不变。
- **首次引导**。`claude auth login` 只保存凭据，不会把 Claude 的首次引导标记为完成，下一次交互启动时又会要求你登录。所以通过 CCRouter 登录成功后，以及启动一个已登录但从未完成引导的账户之前，CCRouter 会在该账户的 `.claude.json` 中写入 `hasCompletedOnboarding` 和 `lastOnboardingVersion`。文件里的其他内容不变，`~/.claude` 也不受影响。
- **额度经由 Claude Code**。`claude -p /usage` 在该账户的环境中运行，不加载工具、MCP 服务器和任何设置来源，也不保存会话。Claude 用自己的锁刷新登录、查询用量，并把结果以 `cachedUsageUtilization` 缓存到该账户的 `.claude.json`。CCRouter 只读取这份缓存，而且只认属于该目录当前登录账户的缓存。这个做法参考了 [CC Switch](https://github.com/farion1231/cc-switch)。
- **用量索引**。一个 FSEvents 事件流监听所有账户的 `projects` 目录。新内容从上次的字节偏移处读起，每条记录计入当时运行这个对话的账户（依据各账户的 `sessions` 进程登记），没有运行时按绑定，同一个消息 ID 只计一次；写到一半的行留到下一次事件再读。

## 网络请求

所有与账户相关的操作都由官方 `claude` CLI 在该账户自己的环境中完成：`claude auth login`、`claude auth status`，以及查询额度的 `claude -p /usage`。

CCRouter 自己发出的请求只有出口 IP 探测，不发送任何遥测数据：

| 目标 | 时机 | 用途 |
| --- | --- | --- |
| `api.anthropic.com/cdn-cgi/trace` | 打开面板时，最多每分钟一次；点刷新时立即查询 | 出口 IP 和所在国家/地区 |

这是 Cloudflare 的诊断页面。请求发往 Anthropic 的域名，但由 Cloudflare 直接返回，不带 Cookie、token 或任何账户信息。选它是因为 Claude Code 连的也是这个域名，代理的分流规则会让两者走同一条线路。

查询额度的 `claude -p /usage` 在打开面板时运行；开启自动调度后，菜单栏还会每 10 分钟在后台运行一次。每个账户最多每 5 分钟一次。

按进程分流或负载均衡的代理仍可能让探测和 Claude 走不同的出口，所以出口 IP 只是参考，不能作为证明。

## 数据与隐私

数据保存在 `~/Library/Application Support/CCRouter`。可以用 `CCROUTER_HOME` 换一个位置，用 `CCROUTER_CLAUDE` 指定 `claude` 可执行文件。

```text
CCRouter/
  state.db    账户、对话绑定、token 统计、缓存的出口 IP
  profiles/   通过 CCRouter 添加的账户的配置目录
  menu.lock   保证只运行一个菜单栏应用
```

`state.db` 不保存 token、提示词或对话正文。凭据由 Claude 自己写入钥匙串或配置目录，CCRouter 从不读取：启动前只检查钥匙串条目是否存在。每个添加的账户各自有独立的 Claude 设置、插件和输入历史；对话记录是共享的。

## 局限

- 菜单栏应用只支持 macOS。启动器和索引也能为 Linux 和 Windows 编译（SQLite 需要 cgo 和目标平台的 C 工具链），但都还没有实机测试。
- 界面目前只有简体中文。
- token 统计来自本地对话记录，不包含其他电脑上的使用，也不包含没有记录用量的请求，因此不等于账单。
- 直接运行的 `claude` 和 IDE 启动的会话不经过 CCRouter 的账户选择。
- 在通过 CCRouter 添加的账户里，Claude 会对每个项目目录询问一次是否信任。这是 Claude 按配置目录分别记录的。
- macOS 26 的 Liquid Glass 还没有在真机上验证。
- 构建使用 ad-hoc 签名且未经公证，下载的应用需要先清除一次隔离标记（见[下载](#下载)）。

## 开发

```sh
make test                                   # 单元与集成测试，开启竞态检测
go vet ./...
dist/ccrouter demo                          # 用演示数据打开面板，与你的账户隔离
dist/ccrouter demo --smoke dist/ui-smoke    # 通过真实的 AppKit 控件点击测试，导出截图和性能样本
make icon                                   # 由 designs/icon/ccrouter.png 重新生成应用图标
```

```text
cmd/ccrouter/        启动器与命令行
cmd/ccrouter-tray/   菜单栏应用
internal/schedule/   自动调度：三种策略、阈值与续聊换号
internal/session/    启动计划、账户环境、登录与引导检查
internal/store/      SQLite 存储与对话绑定
internal/usage/      对话记录增量索引、文件事件、对话标题
internal/status/     经由 claude -p /usage 的额度、出口 IP
internal/icon/       用代码绘制的抽象彼岸花，用于菜单栏
internal/pick/       终端交互列表
internal/ui/         用 Go 编写的 AppKit 界面
tools/icongen/       把原画渲染成 packaging/CCRouter.icns
third_party/darwinkit/  固定版本的 DarwinKit 及兼容性修复
```

界面通过 [DarwinKit](https://github.com/progrium/darwinkit) 调用 AppKit。项目内固定的副本包含针对 Go 1.25 及以上版本 libffi 回调生命周期崩溃的修复（[issue #286](https://github.com/progrium/darwinkit/issues/286)、[PR #289](https://github.com/progrium/darwinkit/pull/289)），以及两处桥接层的小修正，详见 [CCROUTER_PATCHES.md](third_party/darwinkit/CCROUTER_PATCHES.md)。

## 声明

CCRouter 是独立项目，与 Anthropic 没有关联，也未获其认可。Claude 和 Claude Code 是 Anthropic 的商标。CCRouter 默认不会自动换账户。自动调度是默认关闭的可选功能，开启前请自行确认它符合各账户所属套餐的条款。

## 致谢

感谢 [LinuxDo](https://linux.do/) 社区在使用反馈、部署实践和体验改进上的讨论与支持。

## 许可证

[MIT](LICENSE)。内置的 DarwinKit 保留其自己的 [MIT 许可证](third_party/darwinkit/LICENSE)。
