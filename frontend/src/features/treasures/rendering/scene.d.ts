import type { TreasureItem } from '../api';
export interface SceneHandle { setBeach(index:number):void; dispose():void }
export function mountScene(host:HTMLElement,item:TreasureItem,beach:number,onError:(message:string)=>void):SceneHandle;
