"""Bake Blender's spectral Ocean modifier into a reusable displacement atlas.
Run with blender --background --factory-startup --python scripts/bake-ocean.py.
No source image editing: this generates simulation data from an actual Ocean modifier.
"""
import bpy, math, json
import numpy as np
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'public' / 'assets' / 'ocean'
OUT.mkdir(parents=True, exist_ok=True)
bpy.ops.object.select_all(action='SELECT')
bpy.ops.object.delete(use_global=False)
bpy.ops.mesh.primitive_plane_add()
obj=bpy.context.object
mod=obj.modifiers.new('Spectral Ocean','OCEAN')
mod.geometry_mode='GENERATE'
mod.resolution=7
mod.viewport_resolution=7
mod.spatial_size=40
mod.wave_scale=1.2
mod.choppiness=1.0
mod.wind_velocity=9
mod.wave_scale_min=.04
mod.wave_alignment=.35
mod.wave_direction=0
mod.use_foam=True
mod.foam_coverage=.15
mod.foam_layer_name='foam'
mod.repeat_x=1
mod.repeat_y=1
frames=32
records=[]
for frame in range(frames):
 mod.time=frame*.25
 bpy.context.view_layer.update()
 evaluated=obj.evaluated_get(bpy.context.evaluated_depsgraph_get())
 mesh=evaluated.to_mesh()
 xyz=np.empty(len(mesh.vertices)*3,dtype=np.float32)
 mesh.vertices.foreach_get('co',xyz)
 xyz=xyz.reshape(-1,3)
 n=round(math.sqrt(len(xyz)))
 if n*n!=len(xyz):
  raise RuntimeError('Unexpected ocean grid dimensions')
 # Blender keeps row-major grid topology after displacement.
 records.append(xyz.reshape(n,n,3).copy())
 evaluated.to_mesh_clear()
 if frame%8==0: print('Ocean frame',frame,flush=True)
size=256
atlas=np.ones((size*4,size*8,4),dtype=np.float32)
for i,grid in enumerate(records):
 idx=np.linspace(0,len(grid)-1,size).astype(int)
 h=grid[idx[:,None],idx[None,:],2]
 dz,dx=np.gradient(h)
 # RGB = normalized height and two slopes; decoded by the runtime.
 pixels=np.stack([np.clip(h/3+.5,0,1),np.clip(dx*3+.5,0,1),np.clip(dz*3+.5,0,1),np.ones_like(h)],axis=-1)
 row,col=divmod(i,8)
 atlas[row*size:(row+1)*size,col*size:(col+1)*size]=pixels
image=bpy.data.images.new('Ocean spectral atlas',width=size*8,height=size*4,alpha=True,float_buffer=False)
image.colorspace_settings.name='Non-Color'
image.pixels.foreach_set(atlas.ravel())
image.filepath_raw=str(OUT/'spectral-atlas.png')
image.file_format='PNG'
image.save()
(OUT/'simulation.json').write_text(json.dumps({'generator':'Blender '+bpy.app.version_string,'modifier':'Ocean spectral simulation','frames':frames,'tileSize':size,'columns':8,'rows':4,'stepSeconds':.25,'periodSeconds':8,'windVelocity':9,'spatialSize':40,'heightDecode':'(r - 0.5) * 3','slopeDecode':'(gb - 0.5) / 3','loop':'Ping-pong interpolation avoids discontinuous loop endpoints'},indent=2))
bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/'sources'/'ocean.blend'))
print('Ocean atlas saved',flush=True)
