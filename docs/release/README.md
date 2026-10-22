# Learn-With-AI 安装与使用

Windows 下载与 CPU 匹配的 MSI，双击完成安装，从桌面或开始菜单打开独立的 Learn-With-AI 应用窗口。安装包包含前端、Go 程序、文档解析器、教学生成提示词和许可证，不需要安装 Go、Node.js、Python 或 Office。

| 系统 | 分发文件 | 安装与打开 |
| --- | --- | --- |
| Windows Intel / AMD 64 位 | learn-with-ai-0.7.19-windows-amd64.msi | 双击 MSI，按当前用户安装；从图标打开原生应用窗口 |
| Windows ARM64 | learn-with-ai-0.7.19-windows-arm64.msi | 同上，需要 Windows ARM64 |
| macOS | darwin-…-app.tar.gz / 原生 CI 生成的 .pkg | 放进 Applications 后打开；当前 macOS 版本仍使用默认浏览器界面 |
| Debian / Ubuntu | linux-….deb | sudo apt install ./文件名.deb；当前 Linux 版本仍使用默认浏览器界面 |
| 其他 Linux | linux-…-install.tar.gz | 解压后运行 ./install.sh，从应用菜单打开；界面同上 |

## Windows 安装、覆盖与卸载

如果0.7.9等旧版本更新时提示“新版本缺少安装包”，请从[GitHub正式版本](https://github.com/porridgeowefish/Learn-With-AI/releases/latest)下载适合本机架构的Windows MSI。在托盘退出旧应用后，双击MSI完成升级，再打开应用。该旧更新器不接受仓库更名后的下载地址；重新点击旧版更新按钮无法修复。学习数据保存在独立目录，升级时保留。

macOS可从同一页面下载对应Intel或Apple Silicon的PKG，安装后重新打开应用。

MSI 会检测已安装版本。新版升级会显示版本与覆盖确认；取消时不会开始替换。重复打开同版本 MSI 会进入维护界面，可以修复或卸载；不允许降级覆盖较新版本。桌面和开始菜单的 Learn-With-AI 入口直接指向原生应用 exe，使用统一的蓝色书页与电路图标。

旧版自定义 EXE 安装会单独显示迁移确认。确认后先卸载旧应用文件，再安装 MSI 版本；学习数据保留。这个首次 EXE → MSI 迁移是单向的，新安装若失败需重新运行 MSI。后续 MSI → MSI 升级由 Windows Installer 提供事务回滚。升级前请关闭旧应用。

Windows 程序固定安装到 %LOCALAPPDATA%/Programs/learn-with-ai，安装向导不提供目录选择，命令行 INSTALLFOLDER 也无法覆盖此目录。旧版曾安装到其他目录的应用升级后迁入固定目录；旧目录中的非程序文件保留。默认学习数据目录仍为 %LOCALAPPDATA%/learn-with-ai，与程序分离。可从系统「已安装的应用」或 MSI 维护界面卸载，学习项目、笔记、资料和 API 配置保留。

Windows 应用使用 Microsoft Edge WebView2 Runtime。MSI 会检查该组件；若未安装，会显示明确提示。可从微软 [WebView2 官方页面](https://developer.microsoft.com/microsoft-edge/webview2/) 安装 Evergreen Runtime 后重试。缺少组件时不会转而打开普通浏览器冒充桌面应用。

## 应用和资料

从0.7.19起，应用停止支持旧版专用学习格式。遇到旧格式时显示明确提示，不自动转换或删除磁盘原件。请保留原目录，手动整理需要的内容；当前格式的项目及历史内容继续可用。

Windows 在独立 WebView2 窗口中运行，Go 服务位于同一进程，绑定私有 IPv4 loopback 端口。关闭窗口会隐藏到系统托盘，后台继续运行。单击托盘图标恢复窗口，右键选择“退出”才停止自己的服务。侧栏没有退出按钮。学习偏好和 WebView 用户状态按当前用户保存。

API 与默认模型在侧栏「设置」同页配置，配色在顶部。连接只显示预览，点击打开详情弹窗；提供方预置自动填写地址，可选择模型、填写 Key，自行配置可编辑完整连接参数。复习稿、练习题和学科地图独立调用所选 API，不需要 CLI 助教；练习支持选择题数，答案折叠在每道题下。学科地图可直接生成或根据上传参考资料组织章节。小节优先级为红色Ⅰ（关键必学）、黄色Ⅱ（中等深度）与绿色Ⅲ（深入选学），右上角有图例；旧地图重新生成后获得评级。新任务的教师会主动开始教学，正文顶部直接给出文字进度，每次讲一个知识点。

PDF、Markdown、TXT、PPTX、DOCX、XLSX、CSV 自动本地解析，单文件最大100MiB，上传后自动可供教师查阅。教师按目录、检索和连续原文逐步读取，提供可核对的引用。Office 公式保留结构；PDF 尽量保留位置与数学表达。未绑定云端文档识别连接时，扫描页使用对应平台的免费本地 OCR；准备条件见下方表格，无需 API Key。本地 OCR 在后台连续处理整份未读页面，显示进度并支持取消，重启后从已保存的进度恢复，无需逐批点击。资料页也可手动启动整份识别。已绑定的视觉模型仍按原配置调用，模型整份识别逐页请求并可能产生 API 费用；云端自动识别保留费用预算，已发送请求不会自动重试。资料页显示实际已读和未读页数，不能把只读到封面视为整本已解析。识别结果需要按原页核对，不保证任意教材公式和化学结构完全恢复。旧 DOC/PPT/XLS 请转换为 DOCX/PPTX/XLSX。

创建系统学习项目时，特殊要求可以不填。教师页的“附加信息”支持持续保存、修改或清空当前对话的要求，保存后从下一次回答生效，刷新和重启后保留。

教师、复习稿、练习题和笔记使用统一富文本渲染，支持矩阵、Mermaid 和 SVG；图示可缩放、平移、全屏和导出。交互图形在隔离环境中运行。

已有源码版工作区可通过 --workspace 原工作区绝对路径使用；同一工作区只能由一个应用或开发服务持有，请先关闭旧服务。Windows 的 --no-browser 是明确的无窗口验证模式，不是正常桌面启动方式。可通过 --port 指定开发/验证端口，正常应用自动选择私有端口。

macOS 默认数据为 ~/Library/Application Support/learn-with-ai；Linux 为 $XDG_DATA_HOME/learn-with-ai 或 ~/.local/share/learn-with-ai。这些平台当前保留浏览器运行方式，关闭浏览器不会停止服务，维护者可使用 npm run stop 或 POST /api/system/shutdown 显式停止服务。Linux 用户安装可运行 ~/.local/lib/learn-with-ai/uninstall.sh；Debian 使用 sudo apt remove learn-with-ai；macOS 删除 .app。它们的原生安装验证需对应系统执行，不能从 Windows 交叉构建推断。

## 维护者

仓库 Start-LLL / npm run build / npm start 是快速源码验证工具，保留在仓库中，不进入公开 MSI。开发二进制统一生成到 dist/dev/，发布包位于 dist/releases/，dist/ 根目录不保留 EXE。npm run build:desktop 生成带图标的本机原生开发应用；只有显式执行 npm run desktop:install 才创建原生开发快捷方式，目标是 exe。安装版 MSI 只管理一个正式桌面入口。

使用 node tools/release/build-release.js --version 0.7.19 --installers 在各原生系统通过 --targets 选择该系统的目标构建。Windows MSI 使用固定版本 WiX；首次运行 npm run release:tools 恢复 .NET SDK、WiX 和 UI 扩展。工具默认放在仓库外的 %LOCALAPPDATA%/learn-with-ai-build-tools，不作为应用运行依赖分发。可用 LLL_WINDOWS_TOOLCHAIN_DIR 指定其他仓库外绝对路径；清理仓库 .artifacts 不会删除打包工具。macOS PKG 仍由 macOS 原生任务生成。Windows 资源嵌入需要 Windows 主机，CI 采用各操作系统自己的目标。

GitHub Actions 在 v 开头的标签或勾选 publish 的手动运行中发布 Release（全部平台冒烟测试通过后才发布）；不勾选时只生成构件。本地包附带 SHA256SUMS 与 THIRD_PARTY_NOTICES.txt。当前构件未进行代码签名。

## 查看已安装版本

在「设置 → 应用 → 已安装的应用」中搜索 Learn-With-AI；或在 PowerShell 中执行：

    Get-ItemProperty HKCU:\Software\learn-with-ai\Installer | Select-Object Version, InstallLocation

## 免费本地 OCR 与资料管理

程序按运行系统检测免费 OCR，无需为不同版本填写模型名称或 API Key。

| 系统 | 免费识别方式 | 准备条件 |
| --- | --- | --- |
| Windows | 系统 OCR | 简体中文 OCR 语言包 |
| macOS | 系统 Vision | 安装包已附带识别组件；系统需支持简体中文（推荐 macOS 13 或更新版本） |
| Linux | Tesseract | 安装引擎与 chi_sim 语言包；Ubuntu/Debian 可运行 sudo apt install tesseract-ocr tesseract-ocr-chi-sim |

Linux .deb 将识别软件标为推荐依赖；便携安装不自动改动系统软件。资料页显示当前检测结果与准备提示，安装后可刷新状态。免费识别处理本机文件，云端模型识别仍是可选连接。每次最多八页，结束后提示全份资料的待识别页数。普通 OCR 文字可直接用于地图生成，模型从正文整理章节；地图显示资料覆盖限制，不承诺全书覆盖或公式准确。

资料行的“删除”需要确认。确认后清除应用保存的原件、全部解析版本、页图与检索缓存；已有地图和学习内容保留。

## 地图生成失败

生成失败时会显示实际校验原因。已完成但未通过校验的模型正文会保留为只读失败草稿，可以查看或导出；这些操作只读取本地内容，不再次调用模型，也不会替换已发布地图。旧版本没有保存的失败输出无法恢复。重新生成仍是一次新的模型调用，不会自动执行。
