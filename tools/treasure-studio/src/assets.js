import * as T from 'three';

const shells = [
 ['cockle','鸟蛤','Cockle','clam','#ebbc84',18],
 ['blue-mussel','蓝贻贝','Blue Mussel','mussel','#193b64',8],
 ['limpet','笠贝','Limpet','cap','#d9bb8e',24],
 ['periwinkle','滨螺','Periwinkle','spiral','#c5a77e',3],
 ['clam','文蛤','Clam','clam','#f3e7d0',0],
 ['scallop','扇贝','Scallop','fan','#f4a688',22],
 ['oyster','牡蛎','Oyster','oyster','#c8b9a1',12],
 ['razor-clam','蛏子','Razor Clam','razor','#dba96a',0],
 ['slipper-shell','舟螺','Slipper Shell','slipper','#edc99d',8],
 ['auger','笋螺','Auger','tower','#ecc99a',8],
 ['tower-shell','塔螺','Tower Shell','tower','#cba67d',5],
 ['moon-snail','玉螺','Moon Snail','spiral','#e9d9df',1.8],
 ['olive-shell','榧螺','Olive Shell','olive','#f2d6a7',0],
 ['cowrie','宝螺','Cowrie','cowrie','#f8e8cc',0],
 ['sundial','车轮螺','Sundial','sundial','#dcb687',4],
 ['abalone','鲍壳','Abalone','abalone','#85b9b5',0],
 ['murex','骨螺','Murex','murex','#f3cba9',3],
 ['harp-shell','竖琴螺','Harp Shell','harp','#eec5a1',16],
 ['lion-paw','狮爪贝','Lion Paw','fan','#f59b64',11],
 ['wentletrap','梯螺','Wentletrap','tower','#f5eadd',4.5],
 ['nautilus','鹦鹉螺','Nautilus','nautilus','#efdcb5',2.4],
 ['paper-nautilus','纸鹦鹉螺','Paper Nautilus','nautilus','#fff0dd',2.2],
 ['venus-comb','维纳斯骨螺','Venus Comb','comb','#efc6a0',3],
 ['triton','法螺','Triton','triton','#dc9e64',4],
 ['spider-conch','蜘蛛螺','Spider Conch','spider','#f1c5aa',3],
];
const gems = [
 ['rock-crystal','白水晶','Rock Crystal','point','#edf8ff'],
 ['rose-quartz','粉水晶','Rose Quartz','tumbled','#eda7bd'],
 ['smoky-quartz','烟水晶','Smoky Quartz','cushion','#927356'],
 ['red-jasper','红碧玉','Red Jasper','tumbled','#a33524'],
 ['banded-agate','缠丝玛瑙','Banded Agate','cabochon','#d9a06a'],
 ['amethyst','紫水晶','Amethyst','cluster','#9c53c3'],
 ['citrine','黄水晶','Citrine','pear','#f3b330'],
 ['carnelian','红玉髓','Carnelian','cabochon','#ed7025'],
 ['lapis-lazuli','青金石','Lapis Lazuli','cabochon','#1646a3'],
 ['tiger-eye','虎眼石','Tiger Eye','cabochon','#b88331'],
 ['aquamarine','海蓝宝','Aquamarine','emerald','#7ddce7'],
 ['peridot','橄榄石','Peridot','oval','#a4d933'],
 ['garnet','石榴石','Garnet','round','#be213c'],
 ['moonstone','月光石','Moonstone','cabochon','#d7e5fc'],
 ['labradorite','拉长石','Labradorite','tumbled','#335d76'],
 ['tourmaline','碧玺','Tourmaline','point','#60b37a'],
 ['tanzanite','坦桑石','Tanzanite','trillion','#5e63dc'],
 ['spinel','尖晶石','Spinel','cushion','#ef4482'],
 ['zircon','锆石','Zircon','round','#38bde0'],
 ['imperial-topaz','帝王托帕','Imperial Topaz','emerald','#efb16c'],
 ['diamond','钻石','Diamond','round','#f7fcff'],
 ['ruby','红宝石','Ruby','oval','#db1744'],
 ['emerald','祖母绿','Emerald','emerald','#1bb779'],
 ['sapphire','蓝宝石','Sapphire','cushion','#254dcc'],
 ['black-opal','黑欧泊','Black Opal','cabochon','#102333'],
];
export const tiers=['常见','少见','稀有','史诗','传说'];
export const catalog=[...shells.map((a,i)=>({id:a[0],name:a[1],english:a[2],form:a[3],color:a[4],ribs:a[5],tier:1+Math.floor(i/5),kind:'shell'})),...gems.map((a,i)=>({id:a[0],name:a[1],english:a[2],form:a[3],color:a[4],tier:1+Math.floor(i/5),kind:'gem'}))];
const TAU=Math.PI*2;
const mix=(a,b,t)=>a+(b-a)*t;
const clamp=T.MathUtils.clamp;
function surface(n,m,fn,colorFn) {
 const positions=[],colors=[],uv=[],indices=[];
 for(let i=0;i<=n;i++)for(let j=0;j<=m;j++){
  const u=i/n,v=j/m,p=fn(u,v);
  positions.push(...p);uv.push(u,v);
  if(colorFn){const c=colorFn(u,v,p);colors.push(c.r,c.g,c.b);}
 }
 for(let i=0;i<n;i++)for(let j=0;j<m;j++){const a=i*(m+1)+j,b=a+m+1;indices.push(a,b,a+1,b,b+1,a+1);}
 const g=new T.BufferGeometry();g.setAttribute('position',new T.Float32BufferAttribute(positions,3));
 g.setAttribute('uv',new T.Float32BufferAttribute(uv,2));
 if(colors.length)g.setAttribute('color',new T.Float32BufferAttribute(colors,3));
 g.setIndex(indices);g.computeVertexNormals();return g;
}
function shellColor(item,u,v,p){
 const c=new T.Color(item.color), ivory=new T.Color('#fff3df'),brown=new T.Color('#8d4725');
 const wave=Math.sin(u*TAU*(item.ribs||18)+Math.sin(v*31)*.45);
 let amount=.07*Math.sin(v*240)+.025*Math.sin(u*370);
 if(['nautilus','triton','sundial','tower'].includes(item.form)&&item.id!=='paper-nautilus'&&item.id!=='wentletrap')
  c.lerp(brown,Math.pow(Math.max(0,Math.sin(u*TAU*14+v*7)),12)*.65);
 if(['cowrie','olive'].includes(item.form)){
  const speck=Math.sin(p[0]*31+Math.sin(p[1]*21))*Math.sin(p[1]*37+Math.cos(p[2]*31));
  c.lerp(brown,clamp((speck-.48)*3,0,.8));
 }else c.lerp(ivory,Math.max(0,wave)*.18);
 return c.multiplyScalar(1+amount);
}
function shellMaterial(item,inner=false){
 return new T.MeshPhysicalMaterial({color:inner?'#f7e9df':'#ffffff',vertexColors:!inner,
 roughness:inner?.19:.29,metalness:0,clearcoat:.4,clearcoatRoughness:.24,
 iridescence:inner||['abalone','moon-snail','paper-nautilus'].includes(item.id)?.65:.15,
 iridescenceIOR:1.3,iridescenceThicknessRange:[180,430],side:T.DoubleSide});
}
function mesh(group,geometry,material){const m=new T.Mesh(geometry,material);group.add(m);return m;}
function petalShell(item,group){
 const form=item.form,isFan=form==='fan',isLong=['mussel','razor'].includes(form);
 const width=form==='razor'?.27:form==='mussel'?.58:1;
 const height=form==='razor'?1.45:form==='mussel'?1.25:1;
 const ribs=item.ribs||0;
 // Two thick curved valves; closed at perimeter, open enough to show the nacre.
 for(const side of [-1,1]){
  const g=surface(100,100,(u,v)=>{
   const angle=(isFan?Math.PI*.9:TAU)*u+(isFan?-.45*Math.PI:0);
   const r=v;
   let x=Math.sin(angle)*r*width,y=Math.cos(angle)*r*height;
   if(isFan)y-=.5;
   if(form==='oyster'){x*=1+.10*Math.sin(angle*7);y*=1+.08*Math.cos(angle*9);}
   if(form==='mussel')x*=.7+.3*(y/height+1)/2;
   const ripple=ribs?.035*Math.cos(angle*ribs)*(r*.8+.2):.004*Math.sin(r*170);
   const bulge=(isFan?.24*Math.sin(Math.PI*r):.38*Math.sqrt(Math.max(0,1-r*r)))*(1+ripple*3);
   return [x,y,side*(bulge+.025)+ripple];
  },(u,v,p)=>shellColor(item,u,v,p));
  mesh(group,g,shellMaterial(item));
 }
 if(isFan){
  for(const s of [-1,1]){
   const g=new T.SphereGeometry(.17,20,16);g.scale(1.35,.55,.5);g.translate(s*.3,-.38,0);
   const m=shellMaterial(item,true);m.color.set(item.color);mesh(group,g,m);
  }
 }
 group.rotation.x=-.15;
}
function bowlShell(item,group){
 const isCap=item.form==='cap',abalone=item.form==='abalone';
 const fn=(u,v,inner)=>{
  const a=u*TAU,r=v;
  const rib=1+.035*Math.cos(a*(item.ribs||30));
  const x=r*Math.cos(a)*rib*(abalone?1.15:1);
  const y=r*Math.sin(a)*rib*(item.form==='slipper'?.7:1);
  const z=isCap?.7*(1-r):-.4*(1-r*r);
  return [x,y,z+(inner?.035:0)];
 };
 mesh(group,surface(128,64,(u,v)=>fn(u,v,false),(u,v,p)=>shellColor(item,u,v,p)),shellMaterial(item));
 mesh(group,surface(128,64,(u,v)=>fn(u,v,true)),shellMaterial(item,true));
 if(abalone){
  // Raised breathing apertures are explicit geometry, not painted dots.
  for(let i=0;i<6;i++){
   const a=.2+i*.24,g=new T.TorusGeometry(.045,.017,10,20);
   g.translate(.85*Math.cos(a),.76*Math.sin(a),-.08);
   const m=shellMaterial(item,true);m.color.set('#8e9d8e');mesh(group,g,m);
  }
 }
 if(item.form==='slipper'){
  const g=new T.SphereGeometry(.7,36,20,0,Math.PI);g.scale(1,.6,.05);g.translate(0,-.2,-.12);
  mesh(group,g,shellMaterial(item,true));
 }
 group.rotation.x=.2;
}
function ovalShell(item,group){
 const elongated=item.form==='olive',harp=item.form==='harp';
 // Leave an actual narrow aperture along the underside rather than a closed ellipsoid.
 const fn=(u,v,inner)=>{
  const theta=u*Math.PI,phi=.16+v*(TAU-.32);
  const ribs=harp?1+.045*Math.cos(phi*16):1;
  return [Math.sin(theta)*Math.cos(phi)*(elongated?.52:.85)*ribs,
    Math.cos(theta)*(elongated?1.4:1.05),
    Math.sin(theta)*Math.sin(phi)*.61*ribs+(inner?.025:0)];
 };
 mesh(group,surface(100,140,(u,v)=>fn(u,v,false),(u,v,p)=>shellColor(item,u,v,p)),shellMaterial(item));
 const inside=mesh(group,surface(80,100,(u,v)=>fn(u,v,true)),shellMaterial(item,true));inside.scale.setScalar(.96);
 if(item.form==='cowrie')for(const s of [-1,1])for(let i=0;i<20;i++){
  const y=-.86+i*.09,x=s*.08;
  const g=new T.SphereGeometry(.055,10,8);g.scale(1.7,.5,.5);g.translate(x,y,.01);
  mesh(group,g,shellMaterial(item,true));
 }
 group.rotation.set(.4,.35,-.3);
}
function spiralShell(item,group){
 const flat=['nautilus','sundial'].includes(item.form),tower=item.form==='tower';
 const glob=item.form==='spiral';
 const turns=item.ribs||3;
 const config={turns:flat?turns:tower?turns:glob?turns:3,
  growth:flat?1.05:tower?.9:glob?.85:1.15, tube:flat?.55:tower?.38:glob?.72:.61,
  height:flat?0:tower?2.9:glob?.65:1.65};
 if(item.id==='paper-nautilus')config.tube=.49;
 if(item.form==='sundial'){config.tube=.19;config.height=.35;}
 const fn=(u,v,inner=false)=>{
  const theta=(u-1)*TAU*config.turns;
  const r=Math.exp((u-1)*config.growth*2.2);
  const a=v*TAU;
  let tube=r*config.tube;
  const ribs=item.id==='paper-nautilus'?1+.035*Math.cos(theta*27):item.id==='wentletrap'?1+.09*Math.cos(theta*14):1+.012*Math.cos(theta*35);
  tube*=ribs*(inner?.88:1);
  let x=(r+tube*Math.cos(a))*Math.cos(theta),y=(r+tube*Math.cos(a))*Math.sin(theta),z=tube*Math.sin(a);
  if(!flat){y=config.height*(.5-u)+tube*Math.sin(a);z=(r+tube*Math.cos(a))*Math.sin(theta);}
  if(item.form==='sundial')z*=.45;
  return [x,y,z];
 };
 mesh(group,surface(280,64,(u,v)=>fn(u,v),(u,v,p)=>shellColor(item,u,v,p)),shellMaterial(item));
 // Interior of the mouth and thick joining lip.
 mesh(group,surface(28,64,(u,v)=>fn(.90+u*.1,v,true)),shellMaterial(item,true));
 mesh(group,surface(1,64,(u,v)=>{
  const a=fn(1,v),b=fn(1,v,true);return a.map((x,i)=>mix(x,b[i],u));
 }),shellMaterial(item,true));
 if(['murex','comb','spider'].includes(item.form)){
  const count=item.form==='comb'?24:item.form==='spider'?7:16;
  for(let i=0;i<count;i++){
   const u=item.form==='spider'?.97:.62+.36*i/count;
   const v=item.form==='comb'?(i%2?.12:.62):i/count;
   const start=new T.Vector3(...fn(u,v));
   const direction=new T.Vector3(Math.cos(v*TAU),.18,Math.sin(v*TAU)).normalize();
   const length=item.form==='spider'?.75:item.form==='comb'?.42:.26;
   const end=start.clone().addScaledVector(direction,length),mid=start.clone().lerp(end,.5);mid.y+=.1;
   const path=new T.CatmullRomCurve3([start,mid,end]);
   const g=new T.TubeGeometry(path,12,item.form==='spider'?.045:.026,8,false);
   const pos=g.attributes.position;
   for(let j=0;j<pos.count;j++){const t=Math.floor(j/9)/12,c=path.getPointAt(t);const q=new T.Vector3().fromBufferAttribute(pos,j).sub(c).multiplyScalar(1-.88*t).add(c);pos.setXYZ(j,q.x,q.y,q.z);}
   g.computeVertexNormals();mesh(group,g,shellMaterial(item,true));
  }
 }
 group.rotation.set(flat?.15:.25,flat?-.25:.65,flat?.25:-.25);
}
function gemMaterial(item){
 const opaque=['red-jasper','banded-agate','lapis-lazuli','tiger-eye','labradorite','black-opal'].includes(item.id);
 const milky=['rose-quartz','moonstone','carnelian'].includes(item.id);
 return new T.MeshPhysicalMaterial({color:'#ffffff',vertexColors:true,metalness:0,
  roughness:opaque?.16:milky?.15:.045,transmission:opaque?0:milky?.48:.94,
  thickness:1.3,ior:item.id==='diamond'?2.417:item.id==='zircon'?1.94:1.65,
  attenuationColor:new T.Color(item.color),attenuationDistance:3.2,
  clearcoat:1,clearcoatRoughness:.07,
  iridescence:['black-opal','labradorite','moonstone'].includes(item.id)?.8:0,
  iridescenceThicknessRange:[150,460],dispersion:item.id==='diamond'?.8:.2,
  side:T.FrontSide});
}
function gemColor(item,p){
 const c=new T.Color(item.color),x=p[0],y=p[1],z=p[2];
 if(item.id==='banded-agate')c.lerp(new T.Color('#fff3d4'),Math.pow(.5+.5*Math.sin((y+.14*Math.sin(x*5)+.08*Math.sin(z*7))*48),3));
 if(item.id==='tiger-eye')c.lerp(new T.Color('#f8d38a'),Math.pow(.5+.5*Math.sin(x*22+y*3),10));
 if(item.id==='lapis-lazuli'){
  const n=Math.sin(x*210+y*76)*Math.cos(z*170-y*115);if(n>.93)c.set('#d7bd6a');
 }
 if(['black-opal','labradorite'].includes(item.id)){
  const n=Math.sin(x*18+Math.sin(z*21))*Math.sin(y*24+Math.cos(x*17));
  if(n>.12){const h=((Math.sin(x*11+y*8+z*17)+1)*.5);c.setHSL(item.id==='black-opal'?h:.46+h*.2,.8,.25+Math.max(0,n)*.27);}
 }
 if(item.id==='tourmaline')c.lerp(new T.Color('#e99fbf'),Math.pow(Math.max(0,1-Math.abs(y)*1.2),3));
 if(['rose-quartz','red-jasper','moonstone'].includes(item.id)){
  const n=Math.sin(x*19+Math.sin(y*11))*Math.cos(z*21-y*17);c.lerp(new T.Color('#fcf6ed'),Math.pow(Math.max(0,n),14)*.5);
 }
 return c;
}
function facetedGeometry(shape){
 // Explicit crown stars, bezel kites, girdle and pavilion mains.
 // Flat optical faces must remain flat: subdividing or beveling them blurs the cut.
 const positions=[];
 const outline=(a,r,y)=>{
  let x=Math.cos(a),z=Math.sin(a);
  if(shape==='oval')z*=1.25;
  if(shape==='pear'){x*=.83-.24*Math.sin(a);z*=1.32;}
  if(shape==='cushion'){x=Math.sign(x)*Math.pow(Math.abs(x),.65);z=Math.sign(z)*Math.pow(Math.abs(z),.65);}
  if(shape==='trillion'){const q=((a+Math.PI/2)%(TAU/3)+TAU/3)%(TAU/3)-Math.PI/3;const f=.65/Math.cos(q);x*=f;z*=f;}
  return new T.Vector3(x*r,y,z*r);
 };
 const polygon=(points)=>{
  const center=points.reduce((v,p)=>v.add(p),new T.Vector3()).multiplyScalar(1/points.length);
  const normal=new T.Vector3().subVectors(points[1],points[0]).cross(new T.Vector3().subVectors(points[2],points[0]));
  if(normal.dot(center)<0)points=[...points].reverse();
  for(let i=1;i<points.length-1;i++)positions.push(...points[0],...points[i],...points[i+1]);
 };
 if(shape==='emerald'){
  const corners=[[-.72,-1.22],[.72,-1.22],[1,-.94],[1,.94],[.72,1.22],[-.72,1.22],[-1,.94],[-1,-.94]];
  const rings=[[.62,.36],[.81,.2],[1,.02],[1,-.035],[.78,-.24],[.46,-.46],[.08,-.6]].map(([r,y])=>corners.map(([x,z])=>new T.Vector3(x*r,y,z*r)));
  polygon(rings[0]);polygon(rings.at(-1));
  for(let k=0;k<rings.length-1;k++)for(let j=0;j<8;j++)polygon([rings[k][j],rings[k][(j+1)%8],rings[k+1][(j+1)%8],rings[k+1][j]]);
 }else{
  const table=[],stars=[],upper=[],lower=[],pavilion=[];
  for(let i=0;i<8;i++){
   table.push(outline(i*TAU/8,.53,.32));
   stars.push(outline((i+.5)*TAU/8,.8,.177));
   pavilion.push(outline((i+.5)*TAU/8,.58,-.325));
  }
  for(let i=0;i<16;i++){upper.push(outline(i*TAU/16,1,0));lower.push(outline(i*TAU/16,1,-.035));}
  polygon(table);const culet=new T.Vector3(0,-.66,0);
  for(let i=0;i<8;i++){
   const next=(i+1)%8,prev=(i+7)%8,e=i*2,o=e+1,en=(e+2)%16;
   polygon([table[i],table[next],stars[i]]);
   polygon([table[i],stars[prev],upper[e],stars[i]]);
   polygon([stars[i],upper[e],upper[o]]);polygon([stars[i],upper[o],upper[en]]);
   polygon([lower[e],pavilion[prev],culet,pavilion[i]]);
   polygon([lower[e],lower[o],pavilion[i]]);polygon([lower[o],lower[en],pavilion[i]]);
  }
  for(let j=0;j<16;j++)polygon([upper[j],upper[(j+1)%16],lower[(j+1)%16],lower[j]]);
 }
 const geometry=new T.BufferGeometry();geometry.setAttribute('position',new T.Float32BufferAttribute(positions,3));geometry.computeVertexNormals();return geometry;
}
function colorGeometry(g,item){
 const a=g.attributes.position,c=[];
 for(let i=0;i<a.count;i++){const color=gemColor(item,[a.getX(i),a.getY(i),a.getZ(i)]);c.push(color.r,color.g,color.b);}
 g.setAttribute('color',new T.Float32BufferAttribute(c,3));return g;
}
function crystalPoint(radius,height){
 const pts=[new T.Vector2(0,-height*.5)];
 for(let i=0;i<=9;i++)pts.push(new T.Vector2(radius*(1+.008*Math.sin(i*3)),mix(-height*.5,height*.23,i/9)));
 for(let i=1;i<=5;i++)pts.push(new T.Vector2(radius*(1-i/5),mix(height*.23,height*.5,i/5)));
 const g=new T.LatheGeometry(pts,6).toNonIndexed();g.computeVertexNormals();return g;
}
function buildGem(item,group){
 const m=gemMaterial(item);
 if(item.form==='point'||item.form==='cluster'){
  const count=item.form==='cluster'?9:1;
  for(let i=0;i<count;i++){
   const g=colorGeometry(crystalPoint(count===1?.55:.25,count===1?2.4:1.1+.7*(.5+.5*Math.sin(i*7))),item);
   const obj=mesh(group,g,m);
   if(count>1){obj.position.set(Math.cos(i*2.4)*.65,.1*Math.sin(i),Math.sin(i*2.4)*.6);obj.rotation.set(.18*Math.sin(i),i,.22*Math.cos(i));}
  }
 }else if(['tumbled','cabochon'].includes(item.form)){
  const g=surface(100,100,(u,v)=>{
   const a=u*TAU,b=v*Math.PI,s=Math.sin(b);
   const r=item.form==='tumbled'?1+s*s*(.07*Math.sin(a*3)*Math.sin(b*2)+.035*Math.sin(a*5+b*4)):1;
   return [r*s*Math.cos(a),r*Math.cos(b)*.65,r*s*Math.sin(a)*1.15];
  });
  colorGeometry(g,item);mesh(group,g,m);
 }else{
  const g=colorGeometry(facetedGeometry(item.form),item);mesh(group,g,m);group.rotation.set(1.38,0,.06);
 }
}
export function createAsset(id){
 const item=catalog.find(x=>x.id===id);if(!item)throw new Error('Unknown asset: '+id);
 const group=new T.Group();group.name=item.id;group.userData={...item,author:'LLL Treasure Studio',version:1};
 if(item.kind==='gem')buildGem(item,group);
 else if(['fan','clam','mussel','razor','oyster'].includes(item.form))petalShell(item,group);
 else if(['cap','abalone','slipper'].includes(item.form))bowlShell(item,group);
 else if(['cowrie','olive','harp'].includes(item.form))ovalShell(item,group);
 else spiralShell(item,group);
 group.updateMatrixWorld(true);
 const box=new T.Box3().setFromObject(group),size=box.getSize(new T.Vector3()),center=box.getCenter(new T.Vector3());
 const root=new T.Group();root.name=id;root.userData={...group.userData};
 group.position.sub(center);root.add(group);
 const scale=2.5/Math.max(size.x,size.y,size.z);root.scale.setScalar(scale);
 root.updateMatrixWorld(true);return root;
}
export function disposeAsset(root){
 const gs=new Set(),ms=new Set(),textures=new Set();
 root.traverse(o=>{if(o.isMesh){gs.add(o.geometry);for(const m of [].concat(o.material))ms.add(m);}});
 gs.forEach(g=>g.dispose());ms.forEach(m=>{for(const [key,value] of Object.entries(m)){if(key!=='envMap'&&value?.isTexture)textures.add(value);}m.dispose();});textures.forEach(t=>t.dispose());
}
