import * as T from 'three';
import { refineMineral } from './mineral-materials.js';
import { MeshBVH, MeshBVHUniformStruct, BVHShaderGLSL, SAH } from 'three-mesh-bvh';

// Local, deterministic jewelry lighting. No remote textures or paid services.
// BVH ray intersection is supplied by three-mesh-bvh (MIT).
export function applyGemOptics(root,item){
 if(item.kind!=='gem')return;
 if(['tumbled','cabochon'].includes(item.form)){root.rotation.x=1.15;root.traverse(o=>{if(o.isMesh){const m=o.material;m.normalMap?.dispose();m.normalMap=null;m.iridescence=0;m.clearcoat=.45;m.roughness=.2;m.envMapIntensity=.65;refineMineral(m,item.id);m.needsUpdate=true;}});return;}
 root.traverse(mesh=>{
  if(!mesh.isMesh)return;
  const geometry=mesh.geometry.clone();
  const bvh=new MeshBVH(geometry,{strategy:SAH,maxLeafSize:4});
  const acceleration=new MeshBVHUniformStruct();acceleration.updateFrom(bvh);
  const tint=new T.Color(({ruby:'#ce1029',garnet:'#ac1429'})[item.id]||item.color);
  const material=new T.ShaderMaterial({
   uniforms:{bvh:{value:acceleration},world:{value:new T.Matrix4()},worldInv:{value:new T.Matrix4()},
    tint:{value:tint},ior:{value:item.id==='diamond'?2.417:item.id==='zircon'?1.94:1.76},
    dispersion:{value:item.id==='diamond'?.007:.0015}},
   vertexShader:`varying vec3 worldPosition;varying vec3 worldNormal;
    void main(){worldPosition=(modelMatrix*vec4(position,1.)).xyz;
    worldNormal=normalize(mat3(modelMatrix)*normal);
    gl_Position=projectionMatrix*viewMatrix*vec4(worldPosition,1.);}`,
   fragmentShader:`precision highp isampler2D;precision highp usampler2D;
    ${BVHShaderGLSL.common_functions}
    ${BVHShaderGLSL.bvh_struct_definitions}
    ${BVHShaderGLSL.bvh_ray_functions}
    uniform BVH bvh;uniform mat4 world;uniform mat4 worldInv;
    uniform vec3 tint;uniform float ior;uniform float dispersion;
    varying vec3 worldPosition;varying vec3 worldNormal;
    float boxLight(vec3 d,vec3 direction,float width,float height){
     vec3 n=normalize(direction),right=normalize(cross(n,vec3(0.,1.,0.))),up=cross(right,n);
     float facing=dot(d,n);vec2 uv=vec2(dot(d,right),dot(d,up))/max(.01,facing);
     return step(0.,facing)*(1.-smoothstep(width,width+.025,abs(uv.x)))*(1.-smoothstep(height,height+.025,abs(uv.y)));
    }
    vec3 lighting(vec3 d){
     vec3 c=mix(vec3(.045,.055,.08),vec3(.22,.26,.34),smoothstep(-.6,.9,d.y));
     c+=vec3(2.3,2.2,2.1)*boxLight(d,vec3(-3.,4.,4.),.38,.8);
     c+=vec3(1.5,1.8,2.4)*boxLight(d,vec3(4.,1.,2.),.12,.95);
     c+=vec3(2.7)*boxLight(d,vec3(1.,3.,-4.),.55,.32);
     c+=vec3(1.3)*boxLight(d,vec3(-4.,-.5,-2.),.3,.8);
     // A ring of narrow studio cards produces moving facet reflections.
     float azimuth=atan(d.z,d.x);
     float cards=pow(.5+.5*cos(azimuth*9.+.6),24.);
     float band=exp(-pow((d.y-.12)*2.2,2.));
     c+=vec3(.8,.86,1.)*cards*band;
     return c;
    }
    vec3 traceGem(vec3 incoming,vec3 normal,float index){
     vec3 direction=refract(incoming,normal,1./index);
     vec3 origin=(worldInv*vec4(worldPosition+direction*.002,1.)).xyz;
     direction=normalize((worldInv*vec4(direction,0.)).xyz);
     float distanceTravelled=0.;
     for(int bounce=0;bounce<8;bounce++){
      uvec4 faces=uvec4(0u);vec3 hitNormal=vec3(0.),bary=vec3(0.);float side=1.,dist=0.;
      if(!bvhIntersectFirstHit(bvh,origin,direction,faces,hitNormal,bary,side,dist))break;
      distanceTravelled+=dist;vec3 hit=origin+direction*dist;
      vec3 exitDirection=refract(direction,hitNormal,index);
      if(dot(exitDirection,exitDirection)>.001){direction=exitDirection;break;}
      direction=reflect(direction,hitNormal);origin=hit+direction*.002;
     }
     vec3 absorption=pow(max(tint,vec3(.018)),vec3(.65+min(distanceTravelled,5.)*.17));
     return lighting(normalize((world*vec4(direction,0.)).xyz))*absorption;
    }
    void main(){
     vec3 incoming=normalize(worldPosition-cameraPosition),normal=normalize(worldNormal);
     vec3 red=traceGem(incoming,normal,ior*(1.-dispersion));
     vec3 green=traceGem(incoming,normal,ior);
     vec3 blue=traceGem(incoming,normal,ior*(1.+dispersion));
     float f0=pow((ior-1.)/(ior+1.),2.);
     float fresnel=f0+(1.-f0)*pow(1.-max(0.,dot(-incoming,normal)),5.);
     gl_FragColor=vec4(mix(vec3(red.r,green.g,blue.b),lighting(reflect(incoming,normal)),fresnel),1.);
     #include <tonemapping_fragment>
     #include <colorspace_fragment>
    }`
  });
  mesh.onBeforeRender=()=>{material.uniforms.world.value.copy(mesh.matrixWorld);material.uniforms.worldInv.value.copy(mesh.matrixWorld).invert();};
  material.addEventListener('dispose',()=>{acceleration.dispose();geometry.dispose();});
  for(const old of [].concat(mesh.material))old.dispose();
  mesh.material=material;
 });
}
