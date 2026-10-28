// Synced from tools/treasure-studio; edit authoring source and run sync-app-assets.mjs.
import * as T from 'three';

const settings={
 'rose-quartz':{roughness:.23,transmission:.28,thickness:1.5,attenuationColor:'#ffe1e8',attenuationDistance:3},
 'red-jasper':{roughness:.31,transmission:0},
 'carnelian':{roughness:.19,transmission:.46,thickness:1.4,attenuationColor:'#ffac65',attenuationDistance:2.5},
 'lapis-lazuli':{roughness:.3,transmission:0},
 'moonstone':{roughness:.24,transmission:.22,thickness:1.6,attenuationColor:'#e8e5dc',attenuationDistance:3}
};
export function refineMineral(material,id){
 const profile=settings[id];if(!profile)return;
 material.color.set('#ffffff');material.iridescence=0;material.clearcoat=.22;
 material.clearcoatRoughness=.24;material.envMapIntensity=.45;
 material.specularIntensity=.65;
 for(const [key,value] of Object.entries(profile)){
  if(key==='attenuationColor')material[key]=new T.Color(value);else material[key]=value;
 }
 if(id==='moonstone'){
  // Approximate adularescence: a broad blue sheen changes with viewing direction.
  material.onBeforeCompile=shader=>{
   shader.vertexShader='varying vec3 crystalView; varying vec3 crystalPosition;\n'+shader.vertexShader;
   shader.vertexShader=shader.vertexShader.replace('#include <begin_vertex>',`#include <begin_vertex>
    crystalPosition=position;crystalView=(inverse(modelMatrix)*vec4(cameraPosition,1.)).xyz-position;`);
   shader.fragmentShader='varying vec3 crystalView; varying vec3 crystalPosition;\n'+shader.fragmentShader;
   shader.fragmentShader=shader.fragmentShader.replace('#include <emissivemap_fragment>',`#include <emissivemap_fragment>
    float sheen=pow(max(0.,dot(normalize(crystalView),normalize(vec3(.3,1.,.25)))),5.);
    float cloud=.65+.35*sin(crystalPosition.x*3.+crystalPosition.z*2.);
    totalEmissiveRadiance+=vec3(.035,.17,.46)*sheen*cloud;`);
  };
  material.customProgramCacheKey=()=> 'moonstone-adularescence-v1';
 }
 material.needsUpdate=true;
}
