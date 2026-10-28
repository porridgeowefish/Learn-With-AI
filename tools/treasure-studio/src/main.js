import * as T from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';
import { catalog, tiers, disposeAsset } from './assets.js';
import { beaches } from './beaches.js';
import { PlateEnvironment } from './plate-environment.js';
import { applyGemOptics } from './gem-optics.js';
import './style.css';

const $=id=>document.getElementById(id),base=import.meta.env.BASE_URL;
const tierColors=['#91a08d','#6f9f81','#638fb1','#a186bc','#b99a61'];
let renderer,envTarget,ocean,asset=null,loadVersion=0,selectedId='diamond',kind='gem',tier=0,mode='display';
let autoRotate=!matchMedia('(prefers-reduced-motion: reduce)').matches,paused=!autoRotate,simTime=0,last=0,fps=0,frames=0,measure=0;
let manifest,modelStats,beachIndex=0,zoom=1,drag=null,closed=false;
const loader=new GLTFLoader(),turntable=new T.Group(),studioScene=new T.Scene();
const studioCamera=new T.PerspectiveCamera(38,1,.1,100);
studioCamera.position.set(0,1.65,6.8);studioCamera.lookAt(0,1.65,0);

const format=n=>n.toLocaleString('en-US');

function fillCatalog(){
 const query=$('search').value.trim().toLowerCase();
 const entries=catalog.filter(a=>a.kind===kind&&(!tier||a.tier===tier)&&(!query||(a.name+' '+a.english).toLowerCase().includes(query)));
 $('catalog').replaceChildren(...entries.map(item=>{
  const button=document.createElement('button');button.className='asset-card'+(item.id===selectedId?' active':'');
  button.dataset.id=item.id;button.setAttribute('aria-label',item.name+' · '+tiers[item.tier-1]);button.setAttribute('aria-pressed',String(item.id===selectedId));
  const image=document.createElement('img');image.src=base+'assets/thumbs/'+item.id+'.webp';image.alt='';image.loading='lazy';
  image.onerror=()=>{image.style.visibility='hidden';};
  const name=document.createElement('span');name.className='name';name.textContent=item.name;
  const dot=document.createElement('span');dot.className='dot';dot.style.setProperty('--tier',tierColors[item.tier-1]);
  button.append(image,name,dot);button.onclick=()=>selectAsset(item.id);return button;
 }));
 if(!entries.length){const p=document.createElement('p');p.className='empty';p.textContent='没有找到这件藏品';$('catalog').append(p);}
}
function fillTiers(){
 $('tier-tabs').replaceChildren(...['全部',...tiers].map((name,i)=>{
  const b=document.createElement('button');b.textContent=name;b.className=i===tier?'active':'';
  b.setAttribute('aria-pressed',String(i===tier));b.onclick=()=>{tier=i;fillTiers();fillCatalog();};return b;
 }));
}
async function selectAsset(id){
 const item=catalog.find(a=>a.id===id);if(!item)return;
 const version=++loadVersion;selectedId=id;$('loading').hidden=false;$('loading').textContent='正在打开 '+item.name+'…';
 fillCatalog();
 try{
  const entry=manifest.assets.find(a=>a.id===id);
  const gltf=await loader.loadAsync(base+entry.url);
  if(version!==loadVersion||closed){disposeAsset(gltf.scene);return;}
  if(asset){turntable.remove(asset);disposeAsset(asset);}
  asset=gltf.scene;turntable.add(asset);turntable.rotation.set(0,0,0);zoom=1;
  asset.traverse(o=>{if(o.isMesh){o.castShadow=false;o.receiveShadow=false;o.material.envMapIntensity=.8;}});
  applyGemOptics(asset,item);
  positionAsset();
  $('asset-name').textContent=item.name;$('asset-english').textContent=item.english;
  $('asset-tier').textContent=tiers[item.tier-1]+' / '+(item.kind==='shell'?'SHELL COLLECTION':'GEM COLLECTION');
  $('download').href=base+entry.url;$('download').download=id+'.glb';
  modelStats=manifest.assets.find(a=>a.id===id);
  $('stage').dataset.asset=id;$('stage').dataset.loaded='true';
  $('loading').hidden=true;updateDiagnostics();
 }catch(error){
  if(version!==loadVersion)return;
  $('loading').textContent='素材加载失败，点击其他藏品或刷新重试';
  console.error('Asset load failed',id,error);
 }
}
function positionAsset(){
 turntable.position.set(0,1.85,0);
 turntable.scale.setScalar(zoom*(innerWidth<720?.72:1));
}
function setMode(next){
 mode=next;
 $('display-mode').classList.toggle('active',mode==='display');$('ocean-mode').classList.toggle('active',mode==='ocean');$('studio-mode').classList.toggle('active',mode==='studio');
 $('stage').classList.toggle('studio',mode==='studio');$('asset-caption').hidden=mode==='ocean';$('rotate').disabled=mode==='ocean';$('download').hidden=mode==='ocean';
 turntable.visible=mode!=='ocean';
 if(mode==='studio')studioScene.add(turntable);
 else ocean.scene.add(turntable);
 positionAsset();$('stage').dataset.mode=mode;updateDiagnostics();
}
function setBeach(index){
 beachIndex=index;ocean.setBeach(index);const p=beaches[index];
 $('beach-title').textContent=p.name;$('beach-subtitle').textContent=p.english;$('beach-eyebrow').textContent=p.eyebrow;
 $('wave').value=p.waves;$('stage').classList.toggle('night',!!p.night);
 $('stage').dataset.beach=p.id;
 document.querySelectorAll('.beach-card').forEach((b,i)=>{b.classList.toggle('active',index===i);b.setAttribute('aria-pressed',String(index===i));});
 renderer.toneMappingExposure=.95;
 updateDiagnostics();
}
function updateDiagnostics(){
 if(!modelStats)return;
 $('diagnostics').textContent=[
  modelStats.english+' · GLB / glTF 2.0',
  '顶点 '+format(modelStats.vertices)+' · 三角面 '+format(modelStats.triangles),
  '文件 '+(modelStats.bytes/1024).toFixed(0)+' KB',
  '实时帧率 '+fps.toFixed(0)+' fps',
  '海滩 '+beaches[beachIndex].name+' · '+(paused?'已暂停':'实时动画'),
  '物理材质 / 独立三维几何',
  '模型重建 / Blender 海浪数据 / 原图合成壁纸'
 ].join('\n');
}
function resize(){
 if(!renderer)return;
 const rect=$('stage').getBoundingClientRect();renderer.setSize(rect.width,rect.height,false);
 ocean.resize(rect.width,rect.height);studioCamera.aspect=rect.width/rect.height;studioCamera.updateProjectionMatrix();
 positionAsset();
}
function immersive(on){
 document.body.classList.toggle('immersive',on);$('exit-immersive').hidden=!on;resize();
}
function initInteraction(){
 document.querySelectorAll('[data-kind]').forEach(b=>b.onclick=()=>{
  kind=b.dataset.kind;tier=0;$('search').value='';
  document.querySelectorAll('[data-kind]').forEach(x=>x.classList.toggle('active',x===b));fillTiers();fillCatalog();
  selectAsset(catalog.find(a=>a.kind===kind).id);
 });
 $('search').addEventListener('input',fillCatalog);
 $('display-mode').onclick=()=>setMode('display');$('ocean-mode').onclick=()=>setMode('ocean');$('studio-mode').onclick=()=>setMode('studio');
 $('rotate').onclick=()=>{autoRotate=!autoRotate;$('rotate').classList.toggle('active',autoRotate);$('rotate').setAttribute('aria-pressed',String(autoRotate));};
 $('rotate').classList.toggle('active',autoRotate);$('rotate').setAttribute('aria-pressed',String(autoRotate));
 const updatePause=()=>{$('pause').textContent=paused?'播放海浪':'暂停海浪';$('pause').setAttribute('aria-pressed',String(paused));};
 $('pause').onclick=()=>{paused=!paused;updatePause();};updatePause();
 $('wave').oninput=()=>{ocean.uniforms.strength.value=Number($('wave').value);};
 $('quality').onchange=()=>{renderer.setPixelRatio(Math.min(devicePixelRatio,Number($('quality').value)));resize();};
 $('reset').onclick=reset;
 $('scene').ondblclick=reset;
 $('scene').addEventListener('pointerdown',e=>{if(mode==='ocean')return;drag={x:e.clientX,y:e.clientY};$('scene').setPointerCapture(e.pointerId);});
 $('scene').addEventListener('pointermove',e=>{if(!drag)return;turntable.rotation.y+=(e.clientX-drag.x)*.008;turntable.rotation.x=T.MathUtils.clamp(turntable.rotation.x+(e.clientY-drag.y)*.006,-1.2,1.2);drag={x:e.clientX,y:e.clientY};});
 $('scene').addEventListener('pointerup',()=>drag=null);$('scene').addEventListener('pointercancel',()=>drag=null);
 $('scene').addEventListener('wheel',e=>{if(mode==='ocean')return;e.preventDefault();zoom=T.MathUtils.clamp(zoom*Math.exp(-e.deltaY*.001),.45,1.8);positionAsset();},{passive:false});
 $('scene').addEventListener('keydown',e=>{
  if(e.key==='ArrowLeft')turntable.rotation.y-=.15;if(e.key==='ArrowRight')turntable.rotation.y+=.15;
  if(e.key==='ArrowUp')turntable.rotation.x-=.15;if(e.key==='ArrowDown')turntable.rotation.x+=.15;
  if(e.key==='+'||e.key==='='){zoom=Math.min(1.8,zoom+.1);positionAsset();}if(e.key==='-'){zoom=Math.max(.45,zoom-.1);positionAsset();}
  if(e.key.startsWith('Arrow'))e.preventDefault();
 });
 $('inspect').onclick=()=>{$('diagnostics').hidden=!$('diagnostics').hidden;$('inspect').setAttribute('aria-expanded',String(!$('diagnostics').hidden));updateDiagnostics();};
 $('immersive').onclick=()=>immersive(true);$('exit-immersive').onclick=()=>immersive(false);
 window.addEventListener('keydown',e=>{if(e.key==='Escape')immersive(false);});
 new ResizeObserver(resize).observe($('stage'));
}
function reset(){turntable.rotation.set(0,0,0);zoom=1;positionAsset();}
async function init(){
 try{
  renderer=new T.WebGLRenderer({canvas:$('scene'),antialias:true,alpha:false,powerPreference:'high-performance'});
  renderer.setPixelRatio(Math.min(devicePixelRatio,1.5));renderer.toneMapping=T.ACESFilmicToneMapping;renderer.outputColorSpace=T.SRGBColorSpace;
  renderer.shadowMap.enabled=true;renderer.shadowMap.type=T.PCFSoftShadowMap;
  const pmrem=new T.PMREMGenerator(renderer),room=new RoomEnvironment();
  envTarget=pmrem.fromScene(room,.04);room.dispose();pmrem.dispose();
  ocean=new PlateEnvironment(renderer);ocean.scene.environment=envTarget.texture;ocean.scene.environmentIntensity=.7;
  studioScene.background=new T.Color('#dddcd7');studioScene.environment=envTarget.texture;studioScene.environmentIntensity=.8;
  studioScene.add(new T.HemisphereLight('#ffffff','#d6cbb2',.65));
  const key=new T.DirectionalLight('#fff4e0',1.5);key.position.set(-3,5,4);studioScene.add(key);
  const response=await fetch(base+'assets/manifest.json');if(!response.ok)throw new Error('缺少素材清单，请先运行 npm run assets');
  manifest=await response.json();
  $('beaches').replaceChildren(...beaches.map((p,i)=>{
   const b=document.createElement('button');b.className='beach-card';b.setAttribute('aria-label',p.name);
   const img=document.createElement('img');img.src=base+'references/'+['01-lagoon','02-sunset','03-moonlight','04-volcanic','05-mediterranean','06-pink-sand'][i]+'.png';img.alt='';
   const label=document.createElement('span');label.textContent=p.name;b.append(img,label);b.onclick=()=>setBeach(i);return b;
  }));
  document.querySelectorAll('[data-kind]').forEach(b=>b.classList.toggle('active',b.dataset.kind===kind));
  fillTiers();initInteraction();setBeach(0);setMode('display');resize();await selectAsset(selectedId);
  $('status').textContent='●  实时展台已就绪';
  $('scene').addEventListener('webglcontextlost',e=>{e.preventDefault();$('fatal').hidden=false;$('fatal').querySelector('p').textContent='显卡上下文已中断，请重新加载。';});
  renderer.setAnimationLoop(now=>{
   const delta=Math.min((now-last)/1000,.05);last=now;
   if(document.hidden)return;
   if(!paused)simTime+=delta;
   if(autoRotate&&!drag&&mode!=='ocean')turntable.rotation.y+=delta*.18;
   ocean.update(simTime);
   if(mode==='studio')renderer.render(studioScene,studioCamera);else ocean.render();
   frames++;if(now-measure>1000){fps=frames*1000/(now-measure);frames=0;measure=now;updateDiagnostics();}
   $('scene').dataset.time=simTime.toFixed(3);
  });
 }catch(error){console.error(error);$('fatal').hidden=false;$('fatal').querySelector('p').textContent=error.message;$('loading').hidden=true;}
}
window.addEventListener('pagehide',()=>{
 closed=true;renderer?.setAnimationLoop(null);if(asset)disposeAsset(asset);ocean?.dispose();envTarget?.dispose();renderer?.dispose();
});
init();
