"""Blender material/detail pass and editable asset library, no external services."""
import bpy, bmesh, math, json, hashlib, sys
import numpy as np
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
ASSETS=ROOT/'public'/'assets'
REFINED=ASSETS/'refined'
TEXTURES=ROOT/'sources'/'textures'
REFINED.mkdir(parents=True,exist_ok=True)
TEXTURES.mkdir(parents=True,exist_ok=True)
manifest=json.loads((ASSETS/'manifest.json').read_text())
bpy.ops.object.select_all(action='SELECT')
bpy.ops.object.delete(use_global=False)
bpy.context.scene.render.engine='CYCLES'
bpy.context.scene.cycles.samples=64
bpy.context.scene.world.color=(.12,.12,.12)
N=1024
v,u=np.mgrid[0:N,0:N].astype(np.float32)/N

def image_from_array(name,rgb,noncolor=False):
 image=bpy.data.images.new(name,width=N,height=N,alpha=True)
 image.colorspace_settings.name='Non-Color' if noncolor else 'sRGB'
 pixels=np.ones((N,N,4),dtype=np.float32)
 rgb=np.clip(rgb,0,1)
 pixels[:,:,:3]=rgb if noncolor else np.where(rgb<=.04045,rgb/12.92,((rgb+.055)/1.055)**2.4)
 image.pixels.foreach_set(pixels.ravel())
 image.filepath_raw=str(TEXTURES/(name+'.png'));image.file_format='PNG';image.save()
 image.pack()
 return image

def hex_rgb(s):
 return np.array([int(s[i:i+2],16)/255 for i in (1,3,5)],dtype=np.float32)

def mineral_pattern(item):
 x=np.sin(v*math.pi)*np.cos(u*math.tau);y=np.cos(v*math.pi);z=np.sin(v*math.pi)*np.sin(u*math.tau)
 def noise(x,y,z):
  ix=np.floor(x);iy=np.floor(y);iz=np.floor(z)
  fx=x-ix;fy=y-iy;fz=z-iz
  fx=fx*fx*(3-2*fx);fy=fy*fy*(3-2*fy);fz=fz*fz*(3-2*fz)
  result=np.zeros_like(x)
  for a in range(2):
   for b in range(2):
    for c in range(2):
     h=np.mod(np.sin((ix+a)*127.1+(iy+b)*311.7+(iz+c)*74.7)*43758.5453,1)
     result+=h*(fx if a else 1-fx)*(fy if b else 1-fy)*(fz if c else 1-fz)
  return result
 def fbm(scale):
  return sum(noise(x*scale*2**i+3.7,y*scale*2**i+7.2,z*scale*2**i)*(.5**i) for i in range(4))/1.875
 broad=fbm(3);fine=fbm(24);warp=fbm(7)
 vein=np.exp(-np.abs(noise(x*4+warp,y*4,z*4)-.5)*95)
 def blend(a,b,f):return np.array(a)*(1-f[:,:,None])+np.array(b)*f[:,:,None]
 kind=item['id']
 if kind=='rose-quartz':
  clouds=np.clip((broad-.3)*1.7,0,1)
  color=blend([.97,.68,.74],[1.,.9,.9],clouds)
  color=color*(1-vein[:,:,None]*.28)+np.array([1.,.91,.9])*vein[:,:,None]*.28
 elif kind=='red-jasper':
  color=blend([.62,.16,.09],[.82,.32,.18],broad)
  cracks=np.exp(-np.abs(noise(x*3+warp*.5,y*3,z*3)-.48)*180)*np.clip((warp-.35)*4,0,1)
  color=color*(1-cracks[:,:,None]*.85)+np.array([.08,.065,.05])*cracks[:,:,None]*.85
  color*= (.88+fine*.24)[:,:,None]
 elif kind=='carnelian':
  color=blend([.82,.24,.055],[1.,.56,.15],np.clip((broad-.2)*1.8,0,1))
  color=color*(1-vein[:,:,None]*.05)+np.array([1.,.55,.18])*vein[:,:,None]*.05
 elif kind=='lapis-lazuli':
  color=blend([.035,.09,.42],[.10,.29,.78],np.clip(broad*1.1,0,1))
  calcite=vein*.25*np.clip((warp-.55)*7,0,1)
  color=color*(1-calcite[:,:,None])+np.array([.62,.69,.78])*calcite[:,:,None]
  cells=np.mod(np.sin(np.floor(x*55)*127.1+np.floor(y*55)*311.7+np.floor(z*55)*74.7)*43758.5453,1)
  gold=np.clip((cells-.91)*17,0,1)*np.clip((fine-.35)*4,0,1)
  color=color*(1-gold[:,:,None])+np.array([.95,.81,.45])*gold[:,:,None]
 else:
  color=blend([.56,.65,.75],[.96,.92,.81],broad)
  color=color*(1-vein[:,:,None]*.14)+vein[:,:,None]*.14
 return image_from_array(kind+'-color',color),image_from_array(kind+'-normal',np.stack([np.ones_like(u)*.5,np.ones_like(u)*.5,np.ones_like(u)],axis=-1),True)

def pattern(item):
 if item['id'] in ['rose-quartz','red-jasper','carnelian','lapis-lazuli','moonstone']:return mineral_pattern(item)
 base=hex_rgb(item['color'])
 grain=np.sin(u*674+np.sin(v*87)*2)*np.sin(v*915+np.cos(u*65))
 flow=v+.01*np.sin(u*31)+.004*np.sin(u*77)
 growth=np.sin(flow*math.pi*260)
 broad=np.sin(u*math.pi*26+np.sin(v*14)*1.2)
 color=np.ones((N,N,3),dtype=np.float32)*base
 if item['kind']=='shell':
  color*= (.97+grain*.018+growth*.024)[:,:,None]
  color=color*(1-.13*np.maximum(broad,0)[:,:,None])+.13*np.maximum(broad,0)[:,:,None]
  if item['form'] in ['nautilus','triton','sundial','tower'] and item['id'] not in ['paper-nautilus','wentletrap']:
   stripe=np.maximum(0,np.sin(u*math.pi*38+np.sin(v*19)*.8))**5
   tan=np.array([.62,.32,.13],dtype=np.float32)
   color=color*(1-stripe[:,:,None]*.72)+tan*stripe[:,:,None]*.72
  if item['form'] in ['cowrie','olive']:
   spots=np.maximum(0,np.sin(u*89+np.sin(v*71)*3)*np.sin(v*101+np.cos(u*83)*2)-.45)*1.7
   color=color*(1-spots[:,:,None]*.8)+np.array([.34,.18,.08])*spots[:,:,None]*.8
  if item['id']=='blue-mussel':
   color*= (.82+.18*np.sin(v*11)**2)[:,:,None]
  height=growth*.12+grain*.035+broad*.12
 elif item['id']=='black-opal':
  # Mineral-scale mosaic flakes, avoiding broad rainbow stripes.
  x=np.sin(v*math.pi)*np.cos(u*math.tau);y=np.cos(v*math.pi);z=np.sin(v*math.pi)*np.sin(u*math.tau)
  def hash3(x,y,z):
   return np.mod(np.sin(x*127.1+y*311.7+z*74.7)*43758.5453,1)
  cellx=np.floor(x*75);celly=np.floor(y*75);cellz=np.floor(z*75)
  fleck=hash3(cellx,celly,cellz)
  regions=np.sin(x*8+np.sin(z*13))*np.cos(y*11+np.sin(x*9))
  palette=np.array([[.02,.2,.66],[.01,.5,.58],[.015,.63,.22],[.65,.075,.02],[.7,.28,.015],[.14,.05,.55]])
  selector=np.clip(((regions+1)*2.99).astype(int),0,5)
  glow=np.clip((fleck-.3)*1.6,0,1)*( .55+.45*np.sin(x*57+y*83+z*41)**2)
  color=np.array([.012,.022,.04])+palette[selector]*glow[:,:,None]
  height=grain*.015
 elif item['id']=='labradorite':
  band=(.5+.5*np.sin(u*17+v*23+np.sin(v*14)*2))**2
  color=color*.35+np.stack([band*.1,band*.45,band*.8],axis=-1)
  height=grain*.03
 elif item['id']=='tiger-eye':
  sx=np.sin(v*math.pi)*np.cos(u*math.tau);sy=np.cos(v*math.pi)
  band=(.5+.5*np.sin(sx*55+np.sin(sy*9)))**3
  color=color*(.35+band[:,:,None]*.8)+np.array([.2,.12,.01])*band[:,:,None]
  height=grain*.02
 elif item['id']=='banded-agate':
  sx=np.sin(v*math.pi)*np.cos(u*math.tau);sy=np.cos(v*math.pi)
  band=(.5+.5*np.sin((sx+.12*np.sin(sy*4)+.03*np.sin(sy*11))*45))**2
  color=color*(1-band[:,:,None]*.75)+np.array([.98,.91,.75])*band[:,:,None]*.75
  height=grain*.01
 elif item['id']=='lapis-lazuli':
  color*= (.8+.15*np.sin(u*21)*np.cos(v*17))[:,:,None]
  speck=np.maximum(0,grain-.82)*5
  color=color*(1-speck[:,:,None])+np.array([.8,.67,.29])*speck[:,:,None]
  height=grain*.02
 else:
  veins=np.maximum(0,np.sin(u*47+np.sin(v*31)*3)*np.cos(v*53+u*17))**14
  color=color*(1-veins[:,:,None]*.2)+veins[:,:,None]*.15
  height=grain*.02
 dy,dx=np.gradient(height)
 normal=np.stack([-dx*7,-dy*7,np.ones_like(dx)],axis=-1)
 normal/=np.linalg.norm(normal,axis=-1,keepdims=True)
 return image_from_array(item['id']+'-color',color),image_from_array(item['id']+'-normal',normal*.5+.5,True)

stones_only='--stones' in sys.argv or '--focus' in sys.argv
focus='--focus' in sys.argv
bpy.context.preferences.filepaths.save_version=0
for index,item in enumerate(manifest['assets']):
 if focus and item['id'] not in ['rose-quartz','red-jasper','carnelian','lapis-lazuli','moonstone']:continue
 if stones_only and not(item['kind']=='gem' and item['form'] in ['cabochon','tumbled']):continue
 bpy.ops.object.select_all(action='DESELECT')
 before=set(bpy.data.objects)
 bpy.ops.import_scene.gltf(filepath=str(ASSETS/(item['id']+'.glb')))
 objects=list(set(bpy.data.objects)-before)
 meshes=[o for o in objects if o.type=='MESH']
 textured=item['kind']=='shell' or item['form'] in ['cabochon','tumbled']
 images=pattern(item) if textured else None
 handled=set()
 for obj in meshes:
  # Recalculate degenerate pole normals and weld only exact coincident vertices.
  bm=bmesh.new();bm.from_mesh(obj.data)
  bmesh.ops.remove_doubles(bm,verts=list(bm.verts),dist=.00001)
  bmesh.ops.recalc_face_normals(bm,faces=list(bm.faces));bm.to_mesh(obj.data);bm.free()
  if item['kind']=='gem' and item['form'] not in ['cabochon','tumbled']:
   bpy.context.view_layer.objects.active=obj
   bevel=obj.modifiers.new('Polished facet edges','BEVEL')
   bevel.width=.0025;bevel.segments=2;bevel.limit_method='ANGLE';bevel.angle_limit=.3
   bpy.ops.object.modifier_apply(modifier=bevel.name)
  if item['kind']=='shell' or item['form'] in ['cabochon','tumbled']:
   for poly in obj.data.polygons:poly.use_smooth=True
  has_color=len(obj.data.color_attributes)>0
  for material in obj.data.materials:
   if not material or material in handled:continue
   handled.add(material);material.use_nodes=True
   nodes=material.node_tree.nodes;links=material.node_tree.links
   bsdf=next((n for n in nodes if n.type=='BSDF_PRINCIPLED'),None)
   if not bsdf:continue
   if images and has_color:
    for link in list(bsdf.inputs['Base Color'].links):links.remove(link)
    color_node=nodes.new('ShaderNodeTexImage');color_node.image=images[0];color_node.label='1024px collectible surface'
    links.new(color_node.outputs['Color'],bsdf.inputs['Base Color'])
    normal_node=nodes.new('ShaderNodeTexImage');normal_node.image=images[1]
    normal=nodes.new('ShaderNodeNormalMap');normal.inputs['Strength'].default_value=.35 if item['kind']=='shell' else .12
    links.new(normal_node.outputs['Color'],normal.inputs['Color']);links.new(normal.outputs['Normal'],bsdf.inputs['Normal'])
   if item['kind']=='shell':
    bsdf.inputs['Roughness'].default_value=.26
    bsdf.inputs['Coat Weight'].default_value=.35
    bsdf.inputs['Coat Roughness'].default_value=.18
   else:
    bsdf.inputs['Coat Weight'].default_value=1
    bsdf.inputs['Coat Roughness'].default_value=.035
    if item['id'] in ['rose-quartz','red-jasper','carnelian','lapis-lazuli','moonstone']:
     bsdf.inputs['Roughness'].default_value=.28
     bsdf.inputs['Coat Weight'].default_value=.18
     bsdf.inputs['Coat Roughness'].default_value=.22
    if item['form'] not in ['cabochon','tumbled']:
     bsdf.inputs['Roughness'].default_value=.025
  if images and has_color:
   for color_attribute in list(obj.data.color_attributes):obj.data.color_attributes.remove(color_attribute)
 for obj in objects:obj.select_set(True)
 path=REFINED/(item['id']+'.glb')
 bpy.ops.export_scene.gltf(filepath=str(path),export_format='GLB',use_selection=True,export_apply=True,export_extras=True)
 data=path.read_bytes()
 triangles=sum(sum(len(p.vertices)-2 for p in o.data.polygons) for o in meshes)
 vertices=sum(len(o.data.vertices) for o in meshes)
 item.update(url='assets/refined/'+item['id']+'.glb',bytes=len(data),sha256=hashlib.sha256(data).hexdigest(),triangles=triangles,vertices=vertices,refinement='Blender 5.1 / polished edges / 1024px surface maps')
 # Keep a complete editable library arranged as a museum specimen grid.
 roots=[o for o in objects if o.parent not in objects]
 for root in roots:root.location.x+=(index%10)*4;root.location.y+=(index//10)*4
 print('REFINED',index+1,item['id'],flush=True)
manifest['version']=3 if stones_only else 2
(ASSETS/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2),encoding='utf-8')
bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/'sources'/('five-minerals.blend' if focus else 'polished-stones.blend' if stones_only else 'treasure-library.blend')))
print('Finished Blender refinement: '+('5 minerals' if focus else '9 stones' if stones_only else '50 assets'),flush=True)
