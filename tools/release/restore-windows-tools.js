const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { Readable, Transform } = require("node:stream");
const { pipeline } = require("node:stream/promises");
const { toolchainPaths } = require("./windows-msi");

async function main() {
  if (process.platform !== "win32") throw new Error("Windows packaging tools require Windows");
  require('./temporary-root').prepareTemporaryRoot();
  const tools = toolchainPaths();
  const environment = {
    ...process.env,
    DOTNET_ROOT: tools.dotnetDirectory,
    [`DOTNET_ROOT_${process.arch.toUpperCase()}`]: tools.dotnetDirectory,
    DOTNET_CLI_TELEMETRY_OPTOUT: "1",
  };
  function run(bin, args, cwd = tools.root) {
    const result = spawnSync(bin, args, { cwd, env: environment, encoding: "utf8", windowsHide: true });
    if (result.stdout) process.stdout.write(result.stdout);
    if (result.stderr) process.stderr.write(result.stderr);
    if (result.error || result.status !== 0) throw result.error || new Error(`${bin} failed: ${result.status}`);
    return result.stdout.trim();
  }
  fs.mkdirSync(tools.root, { recursive: true });
  const sdkList = fs.existsSync(tools.dotnet)
    ? spawnSync(tools.dotnet, ["--list-sdks"], { env: environment, encoding: "utf8", windowsHide: true })
    : null;
  if (!sdkList || sdkList.status !== 0 || !sdkList.stdout.split(/\r?\n/).some(line => line.startsWith(tools.dotnetSdkVersion + " "))) {
    const metadataResponse = await fetch("https://builds.dotnet.microsoft.com/dotnet/release-metadata/8.0/releases.json");
    if (!metadataResponse.ok) throw new Error(`SDK metadata download failed: ${metadataResponse.status}`);
    const metadata = await metadataResponse.json();
    const sdk = metadata.releases.flatMap(release => release.sdks || [release.sdk]).find(entry => entry.version === tools.dotnetSdkVersion);
    const architecture = { x64: "x64", arm64: "arm64" }[process.arch];
    const archive = sdk?.files.find(file => file.rid === `win-${architecture}` && file.name.endsWith(".zip"));
    if (!architecture || !archive || !/^[a-f0-9]{128}$/i.test(archive.hash)) throw new Error("Official SDK archive metadata is missing");
    const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "lll-packaging-sdk-"));
    const zip = path.join(temporary, "sdk.zip");
    try {
      console.log(`Downloading .NET SDK ${tools.dotnetSdkVersion} to the external toolchain`);
      const response = await fetch(archive.url);
      if (!response.ok || !response.body) throw new Error(`SDK download failed: ${response.status}`);
      const hash = crypto.createHash("sha512");
      const checksum = new Transform({ transform(chunk, encoding, callback) { hash.update(chunk); callback(null, chunk); } });
      await pipeline(Readable.fromWeb(response.body), checksum, fs.createWriteStream(zip));
      if (hash.digest("hex") !== archive.hash.toLowerCase()) throw new Error("SDK SHA-512 verification failed");
      const extract = spawnSync("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", "Expand-Archive -LiteralPath $env:LLL_SDK_ARCHIVE -DestinationPath $env:LLL_SDK_DESTINATION -Force"], {
        env: { ...environment, LLL_SDK_ARCHIVE: zip, LLL_SDK_DESTINATION: tools.dotnetDirectory },
        stdio: "inherit", windowsHide: true,
      });
      if (extract.error || extract.status !== 0) throw extract.error || new Error("SDK extraction failed");
    } finally {
      if (fs.existsSync(zip)) fs.unlinkSync(zip);
      fs.rmdirSync(temporary);
    }
  }
  run(tools.dotnet, ["--list-sdks"]);
  if (!fs.existsSync(tools.wix)) {
    fs.mkdirSync(tools.wixDirectory, { recursive: true });
    run(tools.dotnet, ["tool", "install", "wix", "--version", tools.wixVersion, "--tool-path", tools.wixDirectory]);
  }
  const extension = path.join(tools.wixDirectory, ".wix", "extensions", "WixToolset.UI.wixext", tools.wixVersion, "wixext6", "WixToolset.UI.wixext.dll");
  if (!fs.existsSync(extension)) run(tools.wix, ["extension", "add", `WixToolset.UI.wixext/${tools.wixVersion}`], tools.wixDirectory);
  const version = run(tools.wix, ["--version"], tools.wixDirectory);
  if (!version.startsWith(tools.wixVersion) || !fs.existsSync(extension)) throw new Error("WiX version or UI extension verification failed");
  console.log(`Tools ready: ${tools.root}`);
}
if (require.main === module) main().catch(error => { console.error(error.message); process.exitCode = 1; });
