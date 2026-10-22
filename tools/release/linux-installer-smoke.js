const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const os=require('node:os');
const {spawnSync}=require('node:child_process');

function preparePreferenceFile(privateFile) {
  fs.mkdirSync(path.dirname(privateFile),{recursive:true});
  fs.writeFileSync(privateFile,'retained learning preference');
}

function main() {
if(process.platform!=='linux') throw new Error('Linux installer smoke requires Linux');
require('./temporary-root').prepareTemporaryRoot();
const bundle=path.resolve(process.argv[2]);
const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'learn-linux-installer-smoke-'));
const home=path.join(scratch,"learner's home");
fs.mkdirSync(home);
const env={...process.env,HOME:home,XDG_DATA_HOME:path.join(home,'data')};
const run=(binary,args)=>{
  const result=spawnSync(binary,args,{env,encoding:'utf8',timeout:120000});
  if(result.error || result.status!==0) throw result.error || new Error(`${binary}: ${result.stdout}\n${result.stderr}`);
};
let installed=false;
const app=path.join(home,'.local','lib','learn-with-ai');
try {
  run('tar',['-xzf',bundle,'-C',scratch]);
  const source=fs.readdirSync(scratch).find(name=>name.startsWith('learn-with-ai-'));
  assert(source,'Installer archive has no root');
  run('/bin/sh',[path.join(scratch,source,'install.sh')]);
  installed=true;
  assert(fs.existsSync(path.join(home,'data','applications','learn-with-ai.desktop')));
  run(path.join(home,'.local','bin','learn-with-ai'),['--version']);
  const extra=path.join(app,'my-note.md');
  fs.writeFileSync(extra,'retained user file');
  const privateFile=path.join(home,'data','learn-with-ai','preferences.md');
  preparePreferenceFile(privateFile);
  run('/bin/sh',[path.join(scratch,source,'install.sh')]);
  run(process.execPath,[path.join(__dirname,'smoke.js'),path.join(app,'app','learn-with-ai')]);
  run('/bin/sh',[path.join(app,'uninstall.sh')]);
  installed=false;
  assert.equal(fs.readFileSync(extra,'utf8'),'retained user file');
  assert.equal(fs.readFileSync(privateFile,'utf8'),'retained learning preference');
  assert(!fs.existsSync(path.join(home,'.local','bin','learn-with-ai')));
  assert(!fs.existsSync(path.join(home,'data','applications','learn-with-ai.desktop')));
  console.log('Linux user install, launcher, upgrade, installed runtime, uninstall and data preservation passed');
} finally {
  if(installed && fs.existsSync(path.join(app,'uninstall.sh'))) run('/bin/sh',[path.join(app,'uninstall.sh')]);
  const relative=path.relative(os.tmpdir(),scratch);
  if(relative.startsWith('learn-linux-installer-smoke-') && !relative.includes(path.sep)) fs.rmSync(scratch,{recursive:true,force:true});
}

}
module.exports={preparePreferenceFile};
if(require.main===module)main();
