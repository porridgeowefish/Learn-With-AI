import { useQuery } from '@tanstack/react-query';
import { http } from '@/shared/client';
export interface TreasureItem { id:string; name:string; english:string; kind:'gem'|'shell'; tier:number; form:string; color:string; url:string }
export interface TreasureReward { id:string; itemId:string; kind:'teacher'|'feynman'|'exchange'; projectSlug:string; projectId:string; projectTitle:string; sourceId:string; messageId:string; createdAt:string; isNew:boolean; seen:boolean; welcome?:boolean; eligible?:boolean; redeemed?:boolean }
export type ExchangeMode = 'gem' | 'credit';
export type ExchangeQuote = { mode: ExchangeMode; itemId: string; quantity: number; version: number; id: string };
export interface RewardExchange {
  points: number; credits: number; used: number;
  policy: { version: number; earn: number[]; dailyCounts: number[]; dailyLimit: number; weeklyLimit: number; prices: number[]; values: number[] };
  history: { id: string; name: string; mode: ExchangeMode; quantity: number; amount: number; time: string }[];
  challenge?: { title: string; subject: string; questions: { prompt: string; options: string[] }[] };
  challengeResult?: { score: number; award: number; answers: number[]; correct: number[]; explanations: string[]; completedAt?: string };
}
export interface Collection { catalog:TreasureItem[]; rewards:TreasureReward[]; exchange?:RewardExchange }
export interface TreasureSource { available:boolean; url:string; content:string }
export const collectionKey=['treasures'] as const;
export const tierNames=['常见','少见','稀有','史诗','传说'];
export const rewardActivityNames = ['完成一份练习', '完成一轮对话'] as const;
export const tierColors=['#586873','#247d4e','#176fa5','#8746ae','#9e670c'];
export const tierTextColor=(tier:number)=>`color-mix(in srgb, ${tierColors[tier-1]} 90%, var(--fg))`;
export const thumbnail=(id:string)=>`/treasures/assets/thumbs/${id}.webp`;
export const portrait = (id: string) => `/treasures/assets/portraits/${id}.webp`;
export function useCollection(){return useQuery({queryKey:collectionKey,queryFn:()=>http.get<Collection>('/api/treasures'),staleTime:5000,refetchOnWindowFocus: query => query.state.data?.exchange ? 'always' : false});}
export const readSource=(id:string)=>http.get<TreasureSource>(`/api/treasures/rewards/${encodeURIComponent(id)}/source`);
export const acknowledge=(id:string)=>http.post(`/api/treasures/rewards/${encodeURIComponent(id)}/seen`,{});
export const redeemTreasure = (quote: ExchangeQuote) => http.post<Collection>('/api/treasures/exchange', quote);
export const submitRewardPractice = (answers: number[]) => http.post<Collection>('/api/treasures/challenge', { answers });
