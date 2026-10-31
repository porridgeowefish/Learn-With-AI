export interface RegionRect { x: number; y: number; width: number; height: number }

export function selectionRect(start: { x: number; y: number }, end: { x: number; y: number }, viewport: { width: number; height: number }): RegionRect {
  const x1 = Math.max(0, Math.min(viewport.width, start.x));
  const y1 = Math.max(0, Math.min(viewport.height, start.y));
  const x2 = Math.max(0, Math.min(viewport.width, end.x));
  const y2 = Math.max(0, Math.min(viewport.height, end.y));
  return { x: Math.min(x1, x2), y: Math.min(y1, y2), width: Math.abs(x2 - x1), height: Math.abs(y2 - y1) };
}
