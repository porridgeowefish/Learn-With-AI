import {mkdir,readFile,writeFile,copyFile} from 'node:fs/promises';
const root=new URL('../../../',import.meta.url),source=new URL('../public/',import.meta.url);
const dest=new URL('frontend/public/treasures/',root),code=new URL('frontend/src/features/treasures/rendering/',root);
await mkdir(code,{recursive:true});await mkdir(dest,{recursive:true});
const manifest=JSON.parse(await readFile(new URL('assets/manifest.json',source),'utf8'));
await writeFile(new URL('backend-go/internal/modules/treasures/catalog.json',root),JSON.stringify(manifest.assets.map(({id,name,english,kind,form,color,tier,url})=>({id,name,english,kind,form,color,tier,url})),null,2)+'\n');
for(const item of manifest.assets){for(const name of [item.url,'assets/thumbs/'+item.id+'.webp']){const target=new URL(name,dest);await mkdir(new URL('./',target),{recursive:true});await copyFile(new URL(name,source),target);}}
for(const name of ['assets/ocean/spectral-atlas.png',...['01-lagoon','02-sunset','03-moonlight','04-volcanic','05-mediterranean','06-pink-sand'].map(n=>'references/'+n+'.png')]){const target=new URL(name,dest);await mkdir(new URL('./',target),{recursive:true});await copyFile(new URL(name,source),target);}
for(const name of ['gem-optics.js','mineral-materials.js','plate-environment.js','beaches.js']){
 let text=await readFile(new URL('../src/'+name,import.meta.url),'utf8');text=text.replaceAll("import.meta.env.BASE_URL+","'/treasures/'+");
 await writeFile(new URL(name,code),'// Synced from tools/treasure-studio; edit authoring source and run sync-app-assets.mjs.\n'+text);
}
console.log('Synced 50 collectible models, thumbnails and 6 beach backgrounds.');
