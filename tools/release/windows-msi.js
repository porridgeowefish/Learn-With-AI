const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { repoRoot } = require("./package-layout");
const WIX_VERSION = "6.0.2";
const DOTNET_SDK_VERSION = "8.0.415";
function toolchainPaths(environment = process.env) {
  const root = environment.LWA_WINDOWS_TOOLCHAIN_DIR || environment.LLL_WINDOWS_TOOLCHAIN_DIR || path.join(
    environment.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local"),
    "learn-with-ai-build-tools",
  );
  if (!path.isAbsolute(root)) throw new Error("Windows toolchain path must be absolute");
  const resolved = path.resolve(root);
  const relative = path.relative(repoRoot, resolved);
  if (relative === "" || (relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative))) {
    throw new Error("Windows toolchain must be outside the repository");
  }
  safePath(resolved);
  const dotnetDirectory = path.join(resolved, "dotnet");
  const wixDirectory = path.join(resolved, "wix", WIX_VERSION);
  return {
    root: resolved,
    dotnetDirectory,
    dotnet: path.join(dotnetDirectory, "dotnet.exe"),
    dotnetSdkVersion: DOTNET_SDK_VERSION,
    wixDirectory,
    wix: path.join(wixDirectory, "wix.exe"),
    wixVersion: WIX_VERSION,
  };
}
function guid(value) {
  const h = crypto
    .createHash("sha256")
    .update(value)
    .digest("hex")
    .slice(0, 32);
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-5${h.slice(13, 16)}-a${h.slice(17, 20)}-${h.slice(20)}`.toUpperCase();
}
function msiIdentity(version, arch, identity = "learn-with-ai") {
  if (
    !/^\d+\.\d+\.\d+$/.test(version) ||
    version.split(".").some((n, i) => Number(n) > (i === 2 ? 65535 : 255))
  )
    throw new Error(
      "MSI version must be numeric major.minor.patch within Windows Installer limits",
    );
  if (!["amd64", "arm64"].includes(arch))
    throw new Error("Unsupported MSI architecture");
  return {
    // A rebuilt MSI has a new PackageCode. Give it a new ProductCode too so
    // the existing same-version MajorUpgrade policy can replace local/CI builds.
    productCode: crypto.randomUUID().toUpperCase(),
    upgradeCode: guid(`${identity}:${arch}:upgrade`),
  };
}
function esc(v) {
  return String(v)
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}
function generateWxs(payload, version, arch, icon, options = {}) {
  const identity = options.identity || "learn-with-ai",
    ids = msiIdentity(version, arch, identity);
  const components = [];
  function walk(folder) {
    return fs
      .readdirSync(folder, { withFileTypes: true })
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((e) => {
        if (e.isSymbolicLink()) throw new Error("Linked MSI payload forbidden");
        const absolute = path.join(folder, e.name),
          id =
            "P" +
            crypto
              .createHash("sha256")
              .update(path.relative(payload, absolute).replaceAll("\\", "/"))
              .digest("hex")
              .slice(0, 24);
        if (e.isDirectory())
          return `<Directory Id="D${id}" Name="${esc(e.name)}">${walk(absolute)}</Directory>`;
        if (!e.isFile()) throw new Error("Nonregular MSI payload forbidden");
        components.push(id);
        return `<Component Id="${id}" Guid="${guid(`${identity}:${arch}:file:${path.relative(payload, absolute).replaceAll("\\", "/")}`)}"><File Id="F${id}" Source="${esc(absolute)}" Name="${esc(e.name)}" KeyPath="yes"/></Component>`;
      })
      .join("\n");
  }
  const files = walk(payload);
  const legacy = identity === "learn-with-ai";
  // Display identity only; directories, registry keys and upgrade codes stay lowercase.
  const display = legacy ? "Learn-With-AI" : identity;
  return `<?xml version="1.0" encoding="UTF-8"?><Wix xmlns="http://wixtoolset.org/schemas/v4/wxs" xmlns:ui="http://wixtoolset.org/schemas/v4/wxs/ui"><Package Name="${esc(display)}" Manufacturer="Learn-With-AI contributors" Version="${version}" ProductCode="${ids.productCode}" UpgradeCode="${ids.upgradeCode}" Language="2052" Scope="perUser" InstallerVersion="500">
 <MajorUpgrade DowngradeErrorMessage="已安装更高版本的 Learn-With-AI，无法降级覆盖。请先卸载更高版本，学习数据会保留。" AllowSameVersionUpgrades="yes" Schedule="afterInstallInitialize"/><MediaTemplate EmbedCab="yes"/>
 <Icon Id="ApplicationIcon" SourceFile="${esc(icon)}"/><Property Id="ARPPRODUCTICON" Value="ApplicationIcon"/><Property Id="ARPHELPLINK" Value="https://github.com/xmz14/lll"/>
 <Property Id="MSIFASTINSTALL" Value="0"/>
 <Property Id="ARPNOMODIFY" Value="1"/><Property Id="ARPNOREPAIR" Value="1"/>
 <Binary Id="StopInstalledHelper" SourceFile="$(var.StopInstalledHelper)"/>
 <CustomAction Id="StopInstalled" BinaryRef="StopInstalledHelper" ExeCommand="&quot;[INSTALLFOLDER].&quot;" Return="check"/>
 <InstallExecuteSequence><Custom Action="StopInstalled" Before="InstallValidate" Condition="NOT UPGRADINGPRODUCTCODE"/></InstallExecuteSequence>
 <Property Id="INSTALLFOLDER" Secure="yes"/>
 <!-- Install beside the learner data under %LOCALAPPDATA% (normally the system drive). Windows' shell
      "Local AppData" folder can be redirected to another drive whose Config.Msi is blocked by security
      software; following the environment keeps upgrades on the drive Windows Installer manages. -->
 <SetDirectory Action="SetFixedInstallFolder" Id="INSTALLFOLDER" Value="[%LOCALAPPDATA]\\Programs\\${esc(identity)}\\" Sequence="both"/>
 <Property Id="MSI_ARCH"><RegistrySearch Id="InstallArchitectureSearch" Root="HKCU" Key="Software\\${esc(identity)}\\Installer" Name="Architecture" Type="raw"/></Property>
 <Launch Condition="Installed OR NOT MSI_ARCH OR MSI_ARCH = &quot;${arch}&quot;" Message="已安装其他处理器架构的版本，请先卸载该应用再切换架构。学习数据会保留。"/>
 <Property Id="MSI_VERSION" ><RegistrySearch Id="MsiVersionSearch" Root="HKCU" Key="Software\\${esc(identity)}\\Installer" Name="Version" Type="raw"/></Property>
 ${legacy ? `<Property Id="LEGACY_VERSION" Secure="yes"><RegistrySearch Id="LegacySearch" Root="HKCU" Key="Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\learn-with-ai" Name="DisplayVersion" Type="raw" Bitness="always32"/></Property><Property Id="LEGACY_INSTALL_LOCATION" Secure="yes"><RegistrySearch Id="LegacyInstallLocation" Root="HKCU" Key="Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\learn-with-ai" Name="InstallLocation" Type="raw" Bitness="always32"/></Property><Property Id="MIGRATELEGACY" Secure="yes"/><Launch Condition="Installed OR NOT LEGACY_VERSION OR UILevel &gt;= 5 OR MIGRATELEGACY = &quot;1&quot;" Message="检测到旧 EXE 安装。请双击此 MSI，在安装向导中确认升级迁移。"/><Binary Id="LegacyMigration" SourceFile="$(var.MigrationHelper)"/><CustomAction Id="MigrateLegacy" BinaryRef="LegacyMigration" ExeCommand="&quot;[LEGACY_INSTALL_LOCATION]&quot;" Execute="deferred" Impersonate="yes" Return="check"/><InstallExecuteSequence><Custom Action="MigrateLegacy" After="InstallInitialize" Condition="NOT Installed AND LEGACY_VERSION AND MIGRATELEGACY = &quot;1&quot;"/></InstallExecuteSequence>` : ""}
 <Property Id="WEBVIEW2"><RegistrySearch Id="WebviewMachine32" Root="HKLM" Key="Software\\Microsoft\\EdgeUpdate\\Clients\\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" Name="pv" Type="raw" Bitness="always32"/><RegistrySearch Id="WebviewUser" Root="HKCU" Key="Software\\Microsoft\\EdgeUpdate\\Clients\\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" Name="pv" Type="raw" Bitness="always32"/></Property>
 <Launch Condition="Installed OR WEBVIEW2" Message="需要 Microsoft Edge WebView2 Runtime。请从 https://developer.microsoft.com/microsoft-edge/webview2/ 安装 Evergreen Runtime 后重新运行安装包。"/>
 <StandardDirectory Id="LocalAppDataFolder"><Directory Id="UserPrograms" Name="Programs"><Directory Id="INSTALLFOLDER" Name="${esc(identity)}">${files}</Directory></Directory></StandardDirectory>
 <StandardDirectory Id="DesktopFolder"/><StandardDirectory Id="ProgramMenuFolder"><Directory Id="ApplicationProgramsFolder" Name="${esc(display)}"/></StandardDirectory>
 <DirectoryRef Id="INSTALLFOLDER"><Component Id="AppIntegration" Guid="${guid(`${identity}:${arch}:integration`)}"><RegistryValue Root="HKCU" Key="Software\\${esc(identity)}\\Installer" Name="Version" Value="[ProductVersion]" Type="string" KeyPath="yes"/><RegistryValue Root="HKCU" Key="Software\\${esc(identity)}\\Installer" Name="Architecture" Value="${arch}" Type="string"/><RegistryValue Root="HKCU" Key="Software\\${esc(identity)}\\Installer" Name="InstallLocation" Value="[INSTALLFOLDER]" Type="string"/><Shortcut Id="DesktopShortcut" Directory="DesktopFolder" Name="${esc(display)}" Target="[INSTALLFOLDER]learn-with-ai.exe" WorkingDirectory="INSTALLFOLDER" Icon="ApplicationIcon"><ShortcutProperty Key="System.AppUserModel.ID" Value="learn-with-ai.Desktop"/></Shortcut><Shortcut Id="MenuShortcut" Directory="ApplicationProgramsFolder" Name="${esc(display)}" Target="[INSTALLFOLDER]learn-with-ai.exe" WorkingDirectory="INSTALLFOLDER" Icon="ApplicationIcon"><ShortcutProperty Key="System.AppUserModel.ID" Value="learn-with-ai.Desktop"/></Shortcut><RemoveFolder Id="RemoveApplicationMenu" Directory="ApplicationProgramsFolder" On="uninstall"/></Component></DirectoryRef>
 <Feature Id="Application" Title="${esc(display)}" Level="1">${components.map((id) => `<ComponentRef Id="${id}"/>`).join("")}<ComponentRef Id="AppIntegration"/></Feature>
 <UI Id="SimpleInstallUI">
 <TextStyle Id="Normal" FaceName="Microsoft YaHei UI" Size="10"/><Property Id="DefaultUIFont" Value="Normal"/>

 <Dialog Id="SimpleWelcomeDlg" Width="370" Height="270" Title="[ProductName] 安装">
 <Control Id="Title" Type="Text" X="20" Y="20" Width="330" Height="30" Text="安装 [ProductName] [ProductVersion]"/>
 <Control Id="Message" Type="Text" X="20" Y="65" Width="330" Height="100" Text="点击安装以安装或覆盖更新。学习数据会保留。继续后，安装程序会停止此安装位置正在运行的应用；请先完成当前操作。软件使用 MIT 许可。"/>
 <Control Id="Cancel" Type="PushButton" X="200" Y="230" Width="70" Height="20" Text="取消" Cancel="yes"><Publish Event="EndDialog" Value="Exit"/></Control>
 <Control Id="Install" Type="PushButton" X="280" Y="230" Width="70" Height="20" Text="安装" Default="yes"><Publish Event="SpawnDialog" Value="UpgradeConfirmDlg" Order="1" Condition="WIX_UPGRADE_DETECTED OR LEGACY_VERSION"/><Publish Event="EndDialog" Value="Return" Order="2"/></Control></Dialog>
 <Dialog Id="UpgradeConfirmDlg" Width="370" Height="270" Title="[ProductName] 更新">
 <Control Id="Message" Type="Text" X="20" Y="45" Width="330" Height="150" Text="已有安装。继续将覆盖更新为 [ProductVersion]，学习数据会保留。旧 EXE 迁移会先卸载旧程序；新安装失败时请重新运行此安装包。"/>
 <Control Id="Cancel" Type="PushButton" X="200" Y="230" Width="70" Height="20" Text="取消" Cancel="yes"><Publish Event="EndDialog" Value="Exit"/></Control><Control Id="Continue" Type="PushButton" X="280" Y="230" Width="70" Height="20" Text="继续安装" Default="yes"><Publish Property="MIGRATELEGACY" Value="1"/><Publish Event="EndDialog" Value="Return"/></Control></Dialog>
 <Dialog Id="AlreadyInstalledDlg" Width="370" Height="200" Title="[ProductName]">
 <Control Id="Message" Type="Text" X="20" Y="35" Width="330" Height="100" Text="此版本已经安装，可以直接打开使用。如需卸载，请前往 Windows 设置 → 应用 → 已安装的应用。"/>
 <Control Id="Close" Type="PushButton" X="280" Y="160" Width="70" Height="20" Text="关闭" Default="yes" Cancel="yes"><Publish Event="EndDialog" Value="Exit"/></Control></Dialog>
 <Dialog Id="SimpleProgressDlg" Width="370" Height="150" Title="[ProductName]" Modeless="yes">
 <Control Id="Message" Type="Text" TabSkip="no" X="20" Y="25" Width="330" Height="40" Text="正在处理，请稍候…"/><Control Id="Progress" Type="ProgressBar" X="20" Y="80" Width="330" Height="15" ProgressBlocks="yes"><Subscribe Event="SetProgress" Attribute="Progress"/></Control></Dialog>
 <Dialog Id="SimpleCompleteDlg" Width="370" Height="180" Title="[ProductName]">
 <Control Id="Message" Type="Text" X="20" Y="35" Width="330" Height="75" Text="操作已完成。"/><Control Id="Close" Type="PushButton" X="280" Y="140" Width="70" Height="20" Text="完成" Default="yes" Cancel="yes"><Publish Event="EndDialog" Value="Return"/></Control></Dialog>
 <Dialog Id="SimpleFailureDlg" Width="370" Height="180" Title="[ProductName]">
 <Control Id="Message" Type="Text" X="20" Y="35" Width="330" Height="75" Text="操作未能完成。请关闭正在运行的应用后重新运行安装包。学习数据会保留。"/><Control Id="Close" Type="PushButton" X="280" Y="140" Width="70" Height="20" Text="关闭" Default="yes" Cancel="yes"><Publish Event="EndDialog" Value="Return"/></Control></Dialog>
 <InstallUISequence>
 <Show Dialog="AlreadyInstalledDlg" Before="ExecuteAction" Condition="Installed AND NOT REMOVE"/>
 <Show Dialog="SimpleWelcomeDlg" Before="AlreadyInstalledDlg" Condition="NOT Installed"/>
 <Show Dialog="SimpleProgressDlg" Before="ExecuteAction"/>
 <Show Dialog="SimpleCompleteDlg" OnExit="success"/><Show Dialog="SimpleFailureDlg" OnExit="error"/>
 </InstallUISequence>
 </UI></Package></Wix>`;
}
function run(bin, args, environment = {}) {
  const tools = toolchainPaths();
  const r = spawnSync(bin, args, {
    cwd:
      path.basename(bin).toLowerCase() === "wix.exe"
        ? path.dirname(bin)
        : repoRoot,
    stdio: "inherit",
    windowsHide: true,
    env: {
      ...process.env,
      ...environment,
      ...(fs.existsSync(tools.dotnet)
        ? {
            DOTNET_ROOT: tools.dotnetDirectory,
            [`DOTNET_ROOT_${process.arch.toUpperCase()}`]: tools.dotnetDirectory,
          }
        : {}),
    },
  });
  if (r.error || r.status !== 0)
    throw r.error || new Error(`${bin} failed: ${r.status}`);
}
function wixTool() {
  const tools = toolchainPaths();
  const location = tools.wixDirectory, exe = tools.wix;
  safePath(exe);
  if (!fs.existsSync(exe)) {
    fs.mkdirSync(location, { recursive: true });
    safePath(location);
    run(fs.existsSync(tools.dotnet) ? tools.dotnet : "dotnet", [
      "tool",
      "install",
      "wix",
      "--version",
      WIX_VERSION,
      "--tool-path",
      location,
    ]);
  }
  return exe;
}
function safePath(file) {
  for (let current = path.resolve(file); ; current = path.dirname(current)) {
    let info;
    try {
      info = fs.lstatSync(current);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (info?.isSymbolicLink())
      throw new Error("Linked MSI path forbidden: " + current);
    if (current === path.dirname(current)) break;
  }
  return file;
}
function licenseRtf(text) {
  return (
    "{\\rtf1\\ansi " +
    text
      .replaceAll("\\", "\\\\")
      .replaceAll("{", "\\{")
      .replaceAll("}", "\\}")
      .replaceAll("\n", "\\par\n") +
    "}"
  );
}
function buildMsi(payload, output, version, arch, options = {}) {
  if (process.platform !== "win32")
    throw new Error("MSI generation requires Windows");
  msiIdentity(version, arch, options.identity);
  if (
    options.identity &&
    !/^learn-with-ai(?:-[a-z0-9-]+)?$/.test(options.identity)
  )
    throw new Error("Invalid MSI identity");
  safePath(output);
  safePath(payload);
  const wix = wixTool();
  const extension = path.join(
    path.dirname(wix),
    ".wix",
    "extensions",
    "WixToolset.UI.wixext",
    WIX_VERSION,
    "wixext6",
    "WixToolset.UI.wixext.dll",
  );
  safePath(extension);
  if (!fs.existsSync(extension))
    run(wix, ["extension", "add", `WixToolset.UI.wixext/${WIX_VERSION}`]);
  const staging = path.join(
    output,
    ".installers",
    `${options.identity || "learn-with-ai"}-${version}-${arch}-msi`,
  );
  safePath(staging);
  fs.mkdirSync(staging, { recursive: true });
  const helper = safePath(path.join(staging, "legacy-migrate.exe"));
  run(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      "-H windowsgui -s -w",
      "-o",
      helper,
      "./tools/release/legacy-migrate",
    ],
    { GOOS: "windows", GOARCH: arch, CGO_ENABLED: "0" },
  );
  const stopHelper = safePath(path.join(staging, "stop-installed.exe"));
  run("go", ["build", "-trimpath", "-ldflags", "-H windowsgui -s -w", "-o", stopHelper, "./tools/release/stop-installed"], { GOOS: "windows", GOARCH: arch, CGO_ENABLED: "0" });
  const license = path.join(staging, "LICENSE.rtf");
  fs.writeFileSync(
    safePath(license),
    licenseRtf(fs.readFileSync(path.join(repoRoot, "LICENSE"), "utf8")),
  );
  const source = path.join(staging, "installer.wxs");
  fs.writeFileSync(
    safePath(source),
    generateWxs(
      payload,
      version,
      arch,
      path.join(repoRoot, "scripts", "assets", "learn-with-ai.ico"),
      options,
    ),
  );
  const msi = path.join(
    output,
    `${options.identity || "learn-with-ai"}-${version}-windows-${arch}.msi`,
  );
  safePath(msi);
  safePath(msi.replace(/\.msi$/, ".wixpdb"));
  run(wix, [
    "build",
    source,
    "-arch",
    arch === "amd64" ? "x64" : "arm64",
    "-ext",
    `WixToolset.UI.wixext/${WIX_VERSION}`,
    "-culture",
    "zh-CN",
    "-d",
    `LicenseRtf=${license}`,
    "-d",
    `MigrationHelper=${helper}`,
    "-d",
    `StopInstalledHelper=${stopHelper}`,
    "-o",
    msi,
  ]);
  return [msi];
}
module.exports = {
  toolchainPaths,
  msiIdentity,
  generateWxs,
  buildMsi,
  WIX_VERSION,
  licenseRtf,
};
if (require.main === module && process.argv.includes("--toolchain")) {
  console.log(JSON.stringify(toolchainPaths()));
}
