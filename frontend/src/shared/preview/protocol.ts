export type PreviewSelection = {
  text: string;
  start: number;
  end: number;
  prefix: string;
  suffix: string;
  rect: { left: number; width: number; bottom: number };
};

export const PREVIEW_HOST_PATTERN = /^lll-preview-[0-9a-f]{32}\.localhost$/;
export const PREVIEW_START_TIMEOUT = 15_000;
export const PREVIEW_HEARTBEAT_TIMEOUT = 8_000;
export const MAX_REVIEW_HTML = 500_000;
