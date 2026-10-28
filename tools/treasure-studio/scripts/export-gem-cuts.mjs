import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {GLTFExporter} from 'three/addons/exporters/GLTFExporter.js';
import {catalog,createAsset,disposeAsset} from '../src/assets.js';
globalThis.FileReader=class{readAsArrayBuffer(blob){blob.arrayBuffer().then(b=>{this.result=b;this.onloadend?.();});}};
const out=new URL('../public/assets/',import.meta.url),manifest=JSON.parse(await readFile(new URL('manifest.json',out),'utf8'));
await mkdir(new URL('jewelry/',out),{recursive:true});
for(const item of catalog.filter(i=>i.kind==='gem')){
 const root=createAsset(item.id),buffer=Buffer.from(await new GLTFExporter().parseAsync(root,{binary:true}));
 const stone=['cabochon','tumbled'].includes(item.form);if(stone)await writeFile(new URL(item.id+'.glb',out),buffer);
 const url='assets/jewelry/'+item.id+'.glb';await writeFile(new URL('jewelry/'+item.id+'.glb',out),buffer);
 let vertices=0,triangles=0;root.traverse(o=>{if(o.isMesh){vertices+=o.geometry.attributes.position.count;triangles+=(o.geometry.index?.count??o.geometry.attributes.position.count)/3;}});
 Object.assign(manifest.assets.find(i=>i.id===item.id),{url,vertices,triangles,bytes:buffer.length,sha256:createHash('sha256').update(buffer).digest('hex'),refinement:'Optical crown and pavilion facets; runtime BVH refraction'});
 disposeAsset(root);
}
manifest.version=3;await writeFile(new URL('manifest.json',out),JSON.stringify(manifest,null,2));
console.log('Exported optical gem cuts');
