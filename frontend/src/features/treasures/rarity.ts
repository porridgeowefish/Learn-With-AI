import type { TreasureItem } from './api';

// Mirrors the server's tier roll. This is conditional on a reward being awarded.
export const tierWeights = [60, 25, 10, 4, 1] as const;

export function itemChance(item: TreasureItem, catalog: TreasureItem[], welcome = false): number {
  const peers = catalog.filter(candidate => candidate.kind === item.kind && candidate.tier === item.tier).length;
  if (!peers || (welcome && item.kind !== 'gem')) return 0;
  return tierWeights[item.tier - 1] / peers / (welcome ? 1 : 2);
}

export function chanceText(chance: number): string {
  return `${Number(chance.toFixed(2))}%`;
}
