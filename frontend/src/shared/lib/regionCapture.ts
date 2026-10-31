export interface RegionRect { x: number; y: number; width: number; height: number }
export interface PixelViewport { x: number; y: number; width: number; height: number }

export function selectionRect(start: { x: number; y: number }, end: { x: number; y: number }, viewport: { width: number; height: number }): RegionRect {
  const x1 = Math.max(0, Math.min(viewport.width, start.x));
  const y1 = Math.max(0, Math.min(viewport.height, start.y));
  const x2 = Math.max(0, Math.min(viewport.width, end.x));
  const y2 = Math.max(0, Math.min(viewport.height, end.y));
  return { x: Math.min(x1, x2), y: Math.min(y1, y2), width: Math.abs(x2 - x1), height: Math.abs(y2 - y1) };
}

export function pixelRect(selection: RegionRect, viewport: { width: number; height: number }, pixels: PixelViewport): RegionRect {
  if (viewport.width <= 0 || viewport.height <= 0 || pixels.width <= 0 || pixels.height <= 0) throw new Error('截图尺寸无效');
  const left = Math.round(pixels.x + selection.x * pixels.width / viewport.width);
  const top = Math.round(pixels.y + selection.y * pixels.height / viewport.height);
  const right = Math.round(pixels.x + (selection.x + selection.width) * pixels.width / viewport.width);
  const bottom = Math.round(pixels.y + (selection.y + selection.height) * pixels.height / viewport.height);
  return { x: left, y: top, width: right - left, height: bottom - top };
}

export async function cropFrame(source: CanvasImageSource, selection: RegionRect, viewport: { width: number; height: number }, pixels: PixelViewport): Promise<Blob> {
  const rect = pixelRect(selection, viewport, pixels);
  if (rect.width < 1 || rect.height < 1) throw new Error('框选区域太小');
  const canvas = document.createElement('canvas');
  canvas.width = rect.width;
  canvas.height = rect.height;
  const context = canvas.getContext('2d');
  if (!context) throw new Error('无法创建截图画布');
  context.drawImage(source, rect.x, rect.y, rect.width, rect.height, 0, 0, rect.width, rect.height);
  const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, 'image/png'));
  if (!blob) throw new Error('无法生成截图');
  return blob;
}
