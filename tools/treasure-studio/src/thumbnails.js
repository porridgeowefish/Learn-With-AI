import * as T from 'three';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { applyGemOptics } from './gem-optics.js';
import { disposeAsset } from './assets.js';
// Reusable authoring helper: renders the actual exported geometry, not reference-image crops.
export async function renderThumbnails(onImage){
 const renderer=new T.WebGLRenderer({antialias:true,alpha:true,preserveDrawingBuffer:true});
 renderer.setSize(192,192);renderer.setPixelRatio(1);renderer.toneMapping=T.ACESFilmicToneMapping;renderer.toneMappingExposure=.95;
 const scene=new T.Scene(),camera=new T.PerspectiveCamera(36,1,.1,30);camera.position.set(0,1,5.8);camera.lookAt(0,0,0);
 const pmrem=new T.PMREMGenerator(renderer),room=new RoomEnvironment(),target=pmrem.fromScene(room,.04);
 scene.environment=target.texture;scene.add(new T.HemisphereLight('#ffffff','#c8b8a4',.65));
 const key=new T.DirectionalLight('#ffffff',1.5);key.position.set(-3,5,4);scene.add(key);
 const loader=new GLTFLoader();
 const manifest=await (await fetch(import.meta.env.BASE_URL+'assets/manifest.json')).json();
 try{
  for(const item of manifest.assets){
   const {scene:object}=await loader.loadAsync(import.meta.env.BASE_URL+item.url);
   applyGemOptics(object,item);object.rotation.y=.15;scene.add(object);renderer.render(scene,camera);
   await onImage(item.id,renderer.domElement.toDataURL('image/webp',.9));
   scene.remove(object);disposeAsset(object);
  }
 }finally{target.dispose();room.dispose();pmrem.dispose();renderer.dispose();}
}
