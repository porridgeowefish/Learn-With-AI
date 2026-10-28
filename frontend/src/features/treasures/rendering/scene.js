/* global window */
import * as T from 'three';
import {GLTFLoader} from 'three/addons/loaders/GLTFLoader.js';
import {RoomEnvironment} from 'three/addons/environments/RoomEnvironment.js';
import {PlateEnvironment} from './plate-environment.js';
import {applyGemOptics} from './gem-optics.js';
function disposeModel(root){const textures=new Set(),geometries=new Set(),materials=new Set();root?.traverse(o=>{if(o.isMesh){geometries.add(o.geometry);for(const m of [].concat(o.material))materials.add(m);}});for(const g of geometries)g.dispose();for(const m of materials){for(const [key,value] of Object.entries(m))if(key!=='envMap'&&value?.isTexture)textures.add(value);m.dispose();}for(const t of textures)t.dispose();}
export function mountScene(host,item,beach,onError){
 const renderer=new T.WebGLRenderer({antialias:true,alpha:false,powerPreference:'high-performance'});renderer.setPixelRatio(Math.min(window.devicePixelRatio,1.5));renderer.outputColorSpace=T.SRGBColorSpace;renderer.toneMapping=T.ACESFilmicToneMapping;renderer.toneMappingExposure=.95;
 renderer.domElement.tabIndex=0;renderer.domElement.setAttribute('aria-label',item.name+'，拖动旋转，滚轮缩放，方向键旋转');host.append(renderer.domElement);
 const env=new PlateEnvironment(renderer),room=new RoomEnvironment(),pmrem=new T.PMREMGenerator(renderer),target=pmrem.fromScene(room,.04);room.dispose();pmrem.dispose();env.scene.environment=target.texture;env.scene.environmentIntensity=.7;env.setBeach(beach);
 const group=new T.Group();group.position.set(0,1.95,0);env.scene.add(group);
 let disposed=false,asset=null,drag=null,time=0,last=window.performance.now(),zoom=.9;
 const reduced=window.matchMedia('(prefers-reduced-motion: reduce)');
 const resize=()=>{const w=host.clientWidth,h=host.clientHeight;if(!w||!h)return;renderer.setSize(w,h);env.resize(w,h);group.scale.setScalar(zoom*(w<600?.73:1));};
 const observer=new window.ResizeObserver(resize);observer.observe(host);resize();
 const canvas=renderer.domElement;
 canvas.onpointerdown=e=>{drag={x:e.clientX,y:e.clientY};canvas.setPointerCapture(e.pointerId);};
 canvas.onpointermove=e=>{if(!drag)return;group.rotation.y+=(e.clientX-drag.x)*.008;group.rotation.x=T.MathUtils.clamp(group.rotation.x+(e.clientY-drag.y)*.006,-1.2,1.2);drag={x:e.clientX,y:e.clientY};};
 canvas.onpointerup=canvas.onpointercancel=()=>drag=null;
 canvas.addEventListener('wheel',e=>{e.preventDefault();zoom=T.MathUtils.clamp(zoom*Math.exp(-e.deltaY*.001),.45,1.4);resize();},{passive:false});
 canvas.onkeydown=e=>{if(e.key==='ArrowLeft')group.rotation.y-=.15;if(e.key==='ArrowRight')group.rotation.y+=.15;if(e.key==='ArrowUp')group.rotation.x-=.15;if(e.key==='ArrowDown')group.rotation.x+=.15;if(['+','=','-'].includes(e.key)){zoom=T.MathUtils.clamp(zoom+(e.key==='-'?-.1:.1),.45,1.4);resize();}if(e.key.startsWith('Arrow'))e.preventDefault();};
 canvas.addEventListener('webglcontextlost',e=>{e.preventDefault();onError('三维显示已中断，请重新打开这件宝物。');});
 new GLTFLoader().load('/treasures/'+item.url,gltf=>{if(disposed){disposeModel(gltf.scene);return;}asset=gltf.scene;applyGemOptics(asset,item);group.add(asset);host.dataset.loaded='true';},undefined,()=>{if(!disposed)onError('暂时无法打开三维宝物，请刷新重试。');});
 renderer.setAnimationLoop(now=>{const dt=Math.min((now-last)/1000,.05);last=now;if(window.document.hidden)return;if(!reduced.matches){time+=dt;if(!drag)group.rotation.y+=dt*.09;}env.update(time);env.render();});
 return {setBeach:i=>env.setBeach(i),dispose:()=>{disposed=true;observer.disconnect();renderer.setAnimationLoop(null);disposeModel(asset);env.dispose();target.dispose();renderer.dispose();canvas.remove();}};
}
