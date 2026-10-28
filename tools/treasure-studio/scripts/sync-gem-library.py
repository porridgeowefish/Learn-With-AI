"""Synchronize current downloadable gems into an editable Blender library."""
import bpy,json
from pathlib import Path
root=Path(__file__).resolve().parents[1]
manifest=json.loads((root/'public/assets/manifest.json').read_text(encoding='utf-8'))
bpy.ops.object.select_all(action='SELECT');bpy.ops.object.delete(use_global=False)
for i,item in enumerate(a for a in manifest['assets'] if a['kind']=='gem'):
 before=set(bpy.data.objects)
 bpy.ops.import_scene.gltf(filepath=str(root/'public'/item['url']))
 objects=set(bpy.data.objects)-before
 for obj in objects:
  if obj.parent not in objects:
   obj.location.x+=(i%5)*4;obj.location.y+=(i//5)*4
bpy.ops.wm.save_as_mainfile(filepath=str(root/'sources/gem-library.blend'))
print('Saved current 25-gem Blender library')
