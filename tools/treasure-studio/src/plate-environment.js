import * as T from 'three';
import { beaches } from './beaches.js';

// Camera-matched scenic plates preserve the approved art. Only the water mask
// is animated using Blender Ocean displacement/normal data; this is a 2.5D
// wallpaper compositor, deliberately not advertised as a navigable 3D coast.
const names=['01-lagoon','02-sunset','03-moonlight','04-volcanic','05-mediterranean','06-pink-sand'];
const profiles=[
 {top:[.39,.28,.22,.21,.21,.23,.23,.25,.30],bottom:[.61,.63,.64,.64,.65,.67,.69,.72,.75]},
 {top:[.28,.26,.24,.23,.23,.23,.23,.23,.25],bottom:[.43,.45,.47,.49,.51,.53,.55,.57,.59]},
 {top:[.38,.33,.31,.30,.30,.30,.30,.30,.30],bottom:[.47,.49,.51,.52,.53,.54,.55,.56,.57]},
 {top:[.44,.35,.28,.27,.27,.27,.30,.36,.43],bottom:[.53,.54,.54,.54,.54,.54,.54,.53,.52]},
 {top:[.47,.38,.28,.25,.23,.23,.25,.35,.47],bottom:[.56,.57,.58,.59,.60,.61,.61,.62,.64]},
 {top:[.39,.32,.27,.26,.26,.26,.26,.27,.35],bottom:[.47,.50,.53,.55,.57,.59,.61,.63,.64]},
];
export class PlateEnvironment {
 constructor(renderer){
  this.renderer=renderer;this.scene=new T.Scene();
  this.camera=new T.PerspectiveCamera(38,1,.1,100);this.camera.position.set(0,1.65,6.8);this.camera.lookAt(0,1.65,0);
  this.loader=new T.TextureLoader();this.textures=[];this.version=0;this.disposed=false;
  this.uniforms={time:{value:0},strength:{value:.7},plate:{value:null},oceanData:{value:null},aspect:{value:1},topLine:{value:profiles[0].top},bottomLine:{value:profiles[0].bottom},ready:{value:0},night:{value:0}};
  this.atlas=this.loader.load(import.meta.env.BASE_URL+'assets/ocean/spectral-atlas.png',()=>{this.uniforms.ready.value=1;});
  this.atlas.colorSpace=T.NoColorSpace;this.atlas.minFilter=T.LinearFilter;this.atlas.magFilter=T.LinearFilter;this.atlas.generateMipmaps=false;
  this.uniforms.oceanData.value=this.atlas;
  const material=new T.ShaderMaterial({uniforms:this.uniforms,depthWrite:false,depthTest:false,toneMapped:false,
   vertexShader:`varying vec2 vUv;void main(){vUv=uv;gl_Position=vec4(position.xy,.99999,1.);}`,
   fragmentShader:`
   uniform sampler2D plate,oceanData;
   uniform float time,strength,aspect,ready,night;
   uniform float topLine[9],bottomLine[9];
   varying vec2 vUv;
   vec3 spectral(vec2 uv,float frame){
    float col=mod(frame,8.),row=floor(frame/8.);
    vec2 q=(clamp(fract(uv),.003,.997)+vec2(col,row))/vec2(8.,4.);
    return texture2D(oceanData,q).rgb;
   }
   void main(){
    float imageAspect=1672./941.;
    vec2 uv=vUv;
    if(aspect<imageAspect)uv.x=(uv.x-.5)*aspect/imageAspect+.5;
    else uv.y=(uv.y-.5)*imageAspect/aspect+.5;
    float column=clamp(uv.x*8.,0.,7.999);
    int k=int(floor(column));
    float topY=mix(topLine[k],topLine[k+1],fract(column));
    float bottomY=mix(bottomLine[k],bottomLine[k+1],fract(column));
    float y=1.-uv.y;
    float water=smoothstep(topY+.006,topY+.027,y)*(1.-smoothstep(bottomY-.025,bottomY,y));
    float depth=clamp((y-topY)/max(.01,bottomY-topY),0.,1.);
    float progress=abs(mod(time*2.,62.)-31.);
    float f0=floor(progress),f1=min(f0+1.,31.);
    vec2 sampleUV=vec2(uv.x*2.6/(.35+depth),depth*1.6-time*.008);
    vec3 data=mix(spectral(sampleUV,f0),spectral(sampleUV,f1),fract(progress));
    vec2 gradient=(data.gb-.5)*2.;
    float amplitude=water*strength*ready;
    vec2 offset=gradient*vec2(.012,.007)*(.15+depth*.85)*amplitude;
    offset.y+=(data.r-.5)*.015*amplitude;
    vec3 color=texture2D(plate,uv+offset).rgb;
    // Restrained optical shading: retain the source palette and luminance.
    float facing=gradient.x*.10+gradient.y*.06;
    color*=1.+facing*amplitude;
    float sparkle=pow(max(0.,1.-length(gradient-vec2(.08,-.12))*2.5),30.);
    color+=vec3(.8,.9,1.)*sparkle*.025*amplitude;
    gl_FragColor=vec4(color,1.);
    #include <colorspace_fragment>
   }`});
  this.plate=new T.Mesh(new T.PlaneGeometry(2,2),material);this.plate.frustumCulled=false;this.plate.renderOrder=-100;this.scene.add(this.plate);
  this.light=new T.DirectionalLight('#fff5e5',1.4);this.light.position.set(-3,4,5);this.scene.add(this.light);
  this.fill=new T.HemisphereLight('#e5f3ff','#d1bca8',.65);this.scene.add(this.fill);
  this.setBeach(0);
 }
 setBeach(index){
  this.index=index;const p=beaches[index],version=++this.version;
  this.uniforms.topLine.value=profiles[index].top;this.uniforms.bottomLine.value=profiles[index].bottom;
  this.uniforms.strength.value=p.waves;this.uniforms.night.value=p.night?1:0;
  this.light.color.set(p.sun);this.light.intensity=p.night?.75:1.4;
  if(this.textures[index])this.uniforms.plate.value=this.textures[index];
  else this.loader.load(import.meta.env.BASE_URL+'references/'+names[index]+'.png',texture=>{
   if(this.disposed){texture.dispose();return;} texture.colorSpace=T.SRGBColorSpace;texture.anisotropy=4;this.textures[index]=texture;
   if(version===this.version)this.uniforms.plate.value=texture;
  });
 }
 resize(w,h){this.camera.aspect=w/h;this.camera.updateProjectionMatrix();this.uniforms.aspect.value=w/h;}
 update(time){this.uniforms.time.value=time;}
 render(){this.renderer.render(this.scene,this.camera);}
 dispose(){this.disposed=true;this.version++;this.plate.geometry.dispose();this.plate.material.dispose();this.atlas.dispose();this.textures.forEach(t=>t?.dispose());}
}
