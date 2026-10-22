const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { spawnSync } = require('node:child_process');

function run(binary, args) {
  const result=spawnSync(binary,args,{windowsHide:true,encoding:'utf8',timeout:120000});
  if(result.error || result.status!==0) throw result.error || new Error(`Installer test failed (${result.status}): ${result.stdout}\n${result.stderr}`);
  return result.stdout;
}
function powershell(script) {
  return run('powershell.exe',['-NoProfile','-NonInteractive','-EncodedCommand',Buffer.from("$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); "+script,'utf16le').toString('base64')]).trim();
}
const literal = value=>`'${value.replaceAll("'","''")}'`;

async function main() {
  if(process.platform!=='win32') throw new Error('Windows installer smoke requires Windows');
  require('./temporary-root').prepareTemporaryRoot();
  const installer=path.resolve(process.argv[2]);
  const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'learn-installer-smoke-'));
  const root=path.join(scratch,'Programs','中文安装目录');
  const identity=`learn-with-ai-test-${process.pid}-${Date.now()}`;
  const workspace=path.join(scratch,'workspace');
  fs.mkdirSync(workspace);
  const privateFile=path.join(workspace,'preferences.md');
  fs.writeFileSync(privateFile,'private preferences retained');
  let installed=false;
  try {
    run(installer,['--silent','--install-dir',root,'--integration-name',identity]);
    installed=true;
    assert(fs.existsSync(path.join(root,'learn-with-ai.exe')));
    const integration=powershell(`$key=Get-ItemProperty -LiteralPath ${literal(`HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${identity}`)}; $shell=New-Object -ComObject WScript.Shell; $desktop=Join-Path ([Environment]::GetFolderPath('DesktopDirectory')) ${literal(identity+'.lnk')}; $menu=Join-Path ([Environment]::GetFolderPath('Programs')) ${literal(identity)}; if(!(Test-Path -LiteralPath $desktop) -or !(Test-Path -LiteralPath (Join-Path $menu 'Uninstall.lnk'))) { throw 'Missing shortcuts' }; $link=$shell.CreateShortcut($desktop); @{location=$key.InstallLocation; target=$link.TargetPath; uninstall=$key.UninstallString} | ConvertTo-Json -Compress`);
    const info=JSON.parse(integration);
    assert.equal(fs.realpathSync.native(info.location).toLowerCase(),fs.realpathSync.native(root).toLowerCase());
    assert.equal(fs.realpathSync.native(info.target).toLowerCase(),fs.realpathSync.native(path.join(root,'learn-with-ai.exe')).toLowerCase());
    assert.match(info.uninstall,/--uninstall/);
    const extra=path.join(root,'user-kept.txt');
    fs.writeFileSync(extra,'not owned by installer');
    run(installer,['--silent','--install-dir',root,'--integration-name',identity]);
    assert.equal(fs.readFileSync(extra,'utf8'),'not owned by installer');
    run(process.execPath,[path.join(__dirname,'smoke.js'),path.join(root,'learn-with-ai.exe')]);
    run(process.execPath,[path.join(__dirname,'source-import-smoke.js'),path.join(root,'learn-with-ai.exe')]);
    run(path.join(root,'Uninstall-learn-with-ai.exe'),['--silent','--install-dir',root,'--integration-name',identity]);
    installed=false;
    for(let attempt=0;attempt<60 && fs.existsSync(path.join(root,'Uninstall-learn-with-ai.exe'));attempt++) await new Promise(resolve=>setTimeout(resolve,100));
    assert(!fs.existsSync(path.join(root,'Uninstall-learn-with-ai.exe')),'Self-deletion did not complete');
    assert(!fs.existsSync(path.join(root,'learn-with-ai.exe')));
    assert.equal(fs.readFileSync(extra,'utf8'),'not owned by installer');
    assert.equal(fs.readFileSync(privateFile,'utf8'),'private preferences retained');
    powershell(`if(Test-Path -LiteralPath ${literal(`HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${identity}`)}) { throw 'Uninstall registry entry remains' }; if(Test-Path -LiteralPath (Join-Path ([Environment]::GetFolderPath('DesktopDirectory')) ${literal(identity+'.lnk')})) { throw 'Desktop shortcut remains' }`);
    console.log('Windows install, Chinese install path, shortcuts, upgrade, installed runtime, uninstall and user-data preservation passed');
  } finally {
    if(installed && fs.existsSync(path.join(root,'Uninstall-learn-with-ai.exe'))) {
      run(path.join(root,'Uninstall-learn-with-ai.exe'),['--uninstall','--silent']);
      await new Promise(resolve=>setTimeout(resolve,1500));
    }
    const relative=path.relative(os.tmpdir(),scratch);
    if(relative.startsWith('learn-installer-smoke-') && !relative.includes(path.sep)) fs.rmSync(scratch,{recursive:true,force:true,maxRetries:10,retryDelay:100});
  }
}
main().catch(error=>{console.error(error.stack);process.exitCode=1;});
