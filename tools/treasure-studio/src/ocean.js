import * as T from 'three';
import { beaches } from './beaches.js';

const noiseGLSL=`
float hash(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}
float noise(vec2 p){vec2 i=floor(p),f=fract(p);f=f*f*(3.-2.*f);return mix(mix(hash(i),hash(i+vec2(1,0)),f.x),mix(hash(i+vec2(0,1)),hash(i+1.),f.x),f.y);}
float fbm(vec2 p){float v=0.,a=.5;for(int i=0;i<5;i++){v+=a*noise(p);p=mat2(.8,-.6,.6,.8)*p*2.03+17.;a*=.5;}return v;}
float shore(vec2 p){return (p.y-2.-.7*sin(p.x*.12)-1.1*sin(p.x*.035))*.105;}
`;
const waveGLSL=`
float wave(vec2 p,float time){
 float h=0.;
 h+=sin(dot(p,vec2(.13,.59))+time*1.12)*.24;
 h+=sin(dot(p,vec2(-.25,.9))+time*1.52)*.12;
 h+=sin(dot(p,vec2(.61,1.51))+time*1.94)*.065;
 h+=sin(dot(p,vec2(-1.7,2.1))+time*2.31)*.035;
 h+=sin(dot(p,vec2(3.7,2.9))+time*3.26)*.015;
 return h;
}
`;
const uniformsFor=p=>({
 time:{value:0},strength:{value:p.waves},sand:{value:new T.Color(p.sand)},
 shallow:{value:new T.Color(p.water)},deep:{value:new T.Color(p.deep)},
 skyColor:{value:new T.Color(p.sky)},horizon:{value:new T.Color(p.horizon)},
 sunColor:{value:new T.Color(p.sun)},sunDir:{value:new T.Vector3(p.sunX,p.sunHeight,-1).normalize()},
 night:{value:p.night?1:0},
});
const skyFunctions=`
vec3 sky(vec3 d){
 float h=clamp(d.y,0.,1.);
 vec3 c=mix(horizon,skyColor,pow(h,.55));
 float sunDot=max(0.,dot(d,sunDir));
 c+=sunColor*pow(sunDot,8.)*.12;
 c+=sunColor*pow(sunDot,900.)*.45;
 c+=sunColor*smoothstep(.99965,.99985,sunDot)*4.;
 vec2 cloudUV=d.xz/max(.12,d.y)*.7;
 float cloud=fbm(cloudUV+vec2(time*.002,0));
 float mask=smoothstep(.51,.72,cloud)*smoothstep(.02,.2,d.y)*(1.-night*.75);
 c=mix(c,mix(sunColor,vec3(.8),.55),mask*.5);
 return c;
}
`;
const commonUniforms=`uniform float time,strength,night;uniform vec3 sand,shallow,deep,skyColor,horizon,sunColor,sunDir;`;

function waterGeometry(){
 const nx=230,nz=260,positions=[],indices=[];
 for(let j=0;j<=nz;j++)for(let i=0;i<=nx;i++){
  const v=j/nz,z=16-600*v*v;
  positions.push((i/nx-.5)*(120+v*900),0,z);
 }
 for(let j=0;j<nz;j++)for(let i=0;i<nx;i++){const a=j*(nx+1)+i,b=a+nx+1;indices.push(a,a+1,b,a+1,b+1,b);}
 const g=new T.BufferGeometry();g.setAttribute('position',new T.Float32BufferAttribute(positions,3));g.setIndex(indices);return g;
}
export class OceanEnvironment {
 constructor(renderer){
  this.renderer=renderer;this.scene=new T.Scene();this.camera=new T.PerspectiveCamera(48,1,.1,1500);
  this.camera.position.set(0,3.4,14);this.camera.lookAt(0,1.2,-45);
  this.uniforms=uniformsFor(beaches[0]);const u=this.uniforms;
  const skyMaterial=new T.ShaderMaterial({uniforms:u,side:T.BackSide,depthWrite:false,
   vertexShader:`varying vec3 direction;void main(){direction=position;gl_Position=projectionMatrix*modelViewMatrix*vec4(position,1.);}`,
   fragmentShader:commonUniforms+noiseGLSL+skyFunctions+`varying vec3 direction;void main(){vec3 d=normalize(direction);gl_FragColor=vec4(sky(d),1.);
   if(night>.5&&d.y>.05){vec2 s=floor(d.xz/(d.y+.3)*700.);float star=step(.9989,hash(s))*pow(max(d.y,0.),.2);gl_FragColor.rgb+=star*.6;}
   #include <tonemapping_fragment>
   #include <colorspace_fragment>
   }`});
  this.sky=new T.Mesh(new T.SphereGeometry(900,32,16),skyMaterial);this.scene.add(this.sky);
  const sandMaterial=new T.ShaderMaterial({uniforms:u,
   vertexShader:noiseGLSL+`varying vec3 world;void main(){vec3 p=position;p.y=shore(p.xz)+.015*sin(p.x*3.);world=p;gl_Position=projectionMatrix*modelViewMatrix*vec4(p,1.);}`,
   fragmentShader:commonUniforms+noiseGLSL+skyFunctions+`varying vec3 world;void main(){
    float grains=noise(world.xz*100.);
    float ripples=sin(world.z*15.+sin(world.x*.7)*2.)*.02;
    float dry=smoothstep(-.04,.48,world.y);
    vec3 color=sand*(.68+.22*fbm(world.xz*2.)+.16*grains+ripples);
    vec3 viewDir=normalize(cameraPosition-world);
    vec3 reflection=sky(reflect(-viewDir,vec3(0,1,0)));
    color=mix(color*.6+reflection*.2,color,dry);
    float glint=pow(max(0.,dot(normalize(viewDir+sunDir),vec3(0,1,0))),260.);
    color+=sunColor*glint*(1.-dry)*.7;
    color*=mix(1.,.43,night);
    gl_FragColor=vec4(color,1.);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
   }`});
  const terrain=waterGeometry();this.sand=new T.Mesh(terrain,sandMaterial);this.scene.add(this.sand);
  const waterMaterial=new T.ShaderMaterial({uniforms:u,transparent:false,
   vertexShader:commonUniforms+noiseGLSL+waveGLSL+`varying vec3 world;void main(){
    vec3 p=position;float attenuation=clamp(-shore(p.xz)*.7+.2,.15,1.);
    p.y=wave(p.xz,time)*strength*attenuation;world=p;
    gl_Position=projectionMatrix*modelViewMatrix*vec4(p,1.);
   }`,
   fragmentShader:commonUniforms+noiseGLSL+waveGLSL+skyFunctions+`varying vec3 world;
   void main(){
    float depth=world.y-shore(world.xz);if(depth<.005)discard;
    float att=clamp(-shore(world.xz)*.7+.2,.15,1.);
    float e=.035;float h=wave(world.xz,time);
    vec3 N=normalize(vec3((h-wave(world.xz+vec2(e,0),time))/e*strength*att,1.,(h-wave(world.xz+vec2(0,e),time))/e*strength*att));
    vec2 micro=vec2(sin(world.x*28.+world.z*19.+time*3.),cos(world.x*17.-world.z*32.+time*2.4));
    N=normalize(N+vec3(micro.x*.022,0,micro.y*.022));
    vec3 V=normalize(cameraPosition-world),R=reflect(-V,N);
    float fresnel=.025+.975*pow(1.-max(dot(V,N),0.),5.);
    vec3 refl=sky(vec3(R.x,abs(R.y),R.z));
    vec3 sandBottom=sand*(.72+.14*noise((world.xz+N.xz*depth)*70.));
    float c1=sin(world.x*3.+sin(world.z*2.+time)+time*.8);
    float c2=sin(world.z*4.+sin(world.x*2.1-time*.7));
    float caustic=pow(clamp(1.-abs(c1+c2)*.7,0.,1.),12.)*.24*exp(-depth*.5);
    vec3 under=mix(sandBottom,shallow,1.-exp(-depth*.55));
    under=mix(under,deep,1.-exp(-depth*.09))+sunColor*caustic;
    vec3 col=mix(under,refl,clamp(fresnel,.08,.92));
    vec3 H=normalize(V+sunDir);
    float spec=pow(max(dot(N,H),0.),420.)*3.5+pow(max(dot(N,H),0.),55.)*.12;
    col+=sunColor*spec;
    float shoreFoam=(1.-smoothstep(.02,.32,depth))*smoothstep(.0,.035,depth);
    float lace=fbm(world.xz*5.+N.xz*3.-time*.1);
    float streak=sin(depth*23.-time*1.3+noise(world.xz*2.)*3.);
    float foam=shoreFoam*smoothstep(.28,.57,lace+streak*.13);
    float breakers=smoothstep(.20,.34,h*strength)*smoothstep(.4,1.5,depth)*(1.-smoothstep(2.,5.,depth));
    foam=max(foam,breakers*smoothstep(.52,.74,lace)*.8);
    col=mix(col,mix(vec3(.94,.97,.95),sunColor,.12),foam*.93);
    col*=mix(1.,.47,night);
    col+=vec3(.015,.22,.48)*foam*night*1.3;
    float haze=1.-exp(-length(world.xz)*.0015);col=mix(col,horizon,haze*.32);
    gl_FragColor=vec4(col,1.);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
   }`});
  this.water=new T.Mesh(waterGeometry(),waterMaterial);this.water.frustumCulled=false;this.scene.add(this.water);
  this.props=new T.Group();this.scene.add(this.props);
  this.ambient=new T.HemisphereLight('#e5f6ff','#cbb893',2);this.scene.add(this.ambient);
  this.sun=new T.DirectionalLight('#fff2dd',3);this.scene.add(this.sun);
  this.setBeach(0);
 }
 setBeach(index){
  this.index=index;const p=beaches[index],values=uniformsFor(p);
  for(const key of Object.keys(values)){if(key==='time')continue;
   if(this.uniforms[key].value?.copy)this.uniforms[key].value.copy(values[key].value);
   else this.uniforms[key].value=values[key].value;
  }
  this.sun.color.set(p.sun);this.sun.position.copy(this.uniforms.sunDir.value).multiplyScalar(100);
  this.sun.intensity=p.night?.7:3;this.ambient.intensity=p.night?.5:2;
  this.scene.fog=new T.FogExp2(p.horizon,p.night?.003:.0019);
  this.props.traverse(o=>{if(o.isMesh){o.geometry.dispose();o.material.dispose();}});this.props.clear();
  const rockColor=new T.Color(p.rock);
  const buildRock=(x,z,sx,sy,sz,seed)=>{
   const g=new T.IcosahedronGeometry(1,4),pos=g.attributes.position,colors=[];
   for(let i=0;i<pos.count;i++){
    const a=new T.Vector3().fromBufferAttribute(pos,i);
    const n=Math.sin(a.x*6+seed)*Math.sin(a.y*8+seed)*Math.cos(a.z*7);
    const b=a.clone().multiplyScalar(1+.13*n+.06*Math.sin(a.x*15+a.y*12));
    pos.setXYZ(i,b.x,b.y,b.z);
    const c=rockColor.clone().multiplyScalar(.72+.22*(a.y+1)+.12*n);
    if(p.land==='islands'&&a.y>.2)c.lerp(new T.Color('#344c29'),.7);
    colors.push(c.r,c.g,c.b);
   }
   g.setAttribute('color',new T.Float32BufferAttribute(colors,3));g.computeVertexNormals();
   const m=new T.MeshStandardMaterial({vertexColors:true,roughness:.95});
   const obj=new T.Mesh(g,m);obj.position.set(x,sy*.16,z);obj.scale.set(sx,sy,sz);this.props.add(obj);
  };
  if(p.land==='islands'){
   for(let i=0;i<8;i++)buildRock(65+i*15,-160-i*7,14,10+Math.sin(i*2)*6,13,i);
   for(let i=0;i<4;i++)buildRock(-130+i*18,-210,22,8+i*3,20,i+4);
  }else if(p.land==='stacks'){
   for(let i=0;i<5;i++)buildRock(35+i*17,-90-i*12,6+i,14+i*4,8,i);
   buildRock(-20,-4,8,4,8,2);
  }else if(p.land==='cliffs'){
   for(const s of [-1,1])for(let i=0;i<7;i++)buildRock(s*(23+i*8),-18-i*16,12,10+Math.sin(i)*3,18,i);
  }else{
   for(let i=0;i<6;i++)buildRock(-70-i*28,-140-i*9,28,9+i*2,18,i);
  }
  // Low foreground rocks ground the shore, outside the central display area.
  if(p.land!=='headlands'){buildRock(-16,3,3,1.5,3,2);buildRock(-19,1,2,1,2,6);}
  if(p.palm)this.addPalm(-11,3);
 }
 addPalm(x,z){
  const trunkPath=new T.CatmullRomCurve3([new T.Vector3(x,.5,z),new T.Vector3(x+.5,4,z),new T.Vector3(x+2,8,z-1),new T.Vector3(x+3,10,z-1.5)]);
  this.props.add(new T.Mesh(new T.TubeGeometry(trunkPath,32,.23,10,false),new T.MeshStandardMaterial({color:'#746451',roughness:1})));
  const origin=trunkPath.getPoint(1);
  for(let i=0;i<9;i++){
   const angle=i/9*Math.PI*2;
   const vertices=[],indices=[];
   for(let j=0;j<=28;j++){
    const t=j/28,r=t*5,width=Math.sin(t*Math.PI)*.6*(j%2?.9:1);
    for(const s of [-1,1]){
     vertices.push(origin.x+Math.cos(angle)*r+Math.sin(angle)*width*s,origin.y+Math.sin(t*Math.PI)*1.3-t*t*2.8,origin.z+Math.sin(angle)*r-Math.cos(angle)*width*s);
    }
    if(j<28){const a=j*2;indices.push(a,a+1,a+2,a+1,a+3,a+2);}
   }
   const g=new T.BufferGeometry();g.setAttribute('position',new T.Float32BufferAttribute(vertices,3));g.setIndex(indices);g.computeVertexNormals();
   this.props.add(new T.Mesh(g,new T.MeshStandardMaterial({color:i%2?'#455f2c':'#617c3a',side:T.DoubleSide,roughness:.8})));
  }
 }
 resize(w,h){this.camera.aspect=w/h;this.camera.updateProjectionMatrix();}
 update(time){this.uniforms.time.value=time;}
 render(){this.renderer.render(this.scene,this.camera);}
 dispose(){
  this.scene.traverse(o=>{if(o.isMesh){o.geometry.dispose();for(const m of [].concat(o.material))m.dispose();}});
 }
}
