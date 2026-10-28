import { mkdir, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js';
import { catalog, createAsset, disposeAsset } from '../src/assets.js';
import { beaches } from '../src/beaches.js';
// GLTFExporter uses the browser FileReader API even with binary-only geometry.
globalThis.FileReader=class {
 readAsArrayBuffer(blob){blob.arrayBuffer().then(b=>{this.result=b;this.onloadend?.();});}
 readAsDataURL(blob){blob.arrayBuffer().then(b=>{this.result='data:'+blob.type+';base64,'+Buffer.from(b).toString('base64');this.onloadend?.();});}
};
const out=new URL('../public/assets/',import.meta.url);
await mkdir(out,{recursive:true});
const exporter=new GLTFExporter(),manifest=[];
for(const item of catalog){
 const object=createAsset(item.id);
 const data=await exporter.parseAsync(object,{binary:true,onlyVisible:true});
 const bytes=Buffer.from(data);
 await writeFile(new URL(item.id+'.glb',out),bytes);
 let triangles=0,vertices=0;
 object.traverse(o=>{if(o.isMesh){vertices+=o.geometry.attributes.position.count;triangles+=(o.geometry.index?.count??o.geometry.attributes.position.count)/3;}});
 manifest.push({...item,url:'assets/'+item.id+'.glb',bytes:bytes.length,triangles,vertices,sha256:createHash('sha256').update(bytes).digest('hex')});
 disposeAsset(object);
}
await writeFile(new URL('manifest.json',out),JSON.stringify({version:1,format:'glTF 2.0 / GLB',assetCount:manifest.length,assets:manifest},null,2));
await writeFile(new URL('beaches.json',out),JSON.stringify(beaches,null,2));
console.log('Exported '+manifest.length+' GLB models and '+beaches.length+' beach presets. '+(manifest.reduce((s,a)=>s+a.bytes,0)/1048576).toFixed(1)+' MiB');
