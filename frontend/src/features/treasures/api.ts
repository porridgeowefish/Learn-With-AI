import { useQuery } from '@tanstack/react-query';
import { http } from '@/shared/client';
export interface TreasureItem { id:string; name:string; english:string; kind:'gem'|'shell'; tier:number; form:string; color:string; url:string }
export interface TreasureReward { id:string; itemId:string; kind:'teacher'|'feynman'; projectSlug:string; projectId:string; projectTitle:string; sourceId:string; messageId:string; createdAt:string; isNew:boolean; seen:boolean; welcome?:boolean }
export interface Collection { catalog:TreasureItem[]; rewards:TreasureReward[] }
export interface TreasureSource { available:boolean; url:string; content:string }
export const collectionKey=['treasures'] as const;
export const tierNames=['常见','少见','稀有','史诗','传说'];
export const tierColors=['#90998b','#6f9676','#568dac','#9a7cac','#bb963c'];
export const thumbnail=(id:string)=>`/treasures/assets/thumbs/${id}.webp`;
export function useCollection(){return useQuery({queryKey:collectionKey,queryFn:()=>http.get<Collection>('/api/treasures'),staleTime:5000});}
export const readSource=(id:string)=>http.get<TreasureSource>(`/api/treasures/rewards/${encodeURIComponent(id)}/source`);
export const acknowledge=(id:string)=>http.post(`/api/treasures/rewards/${encodeURIComponent(id)}/seen`,{});
