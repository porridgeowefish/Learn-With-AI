// Application-wide constants. Pull magic strings out of components.

export const API_BASE = ''; // Same-origin in prod, proxied by Vite in dev.

export const HEALTH_POLL_INTERVAL_MS = 30_000;

// SSE event names emitted by backend-go/internal/httpx/sse.go.
export const SSE_EVENTS = {
  hello: 'hello',
  treasureAwarded: 'treasure-awarded',
  annotationUpdated: 'annotation-updated',
  teacherNoteUpdated: 'teacher-note-updated',
  learningAssetUpdated: 'learning-asset-updated',
  learningActivityUpdated: 'learning-activity-updated',
  sourceUpdated: 'source-updated',
  generationJobUpdated: 'generation-job-updated',
  sourceStructureJobUpdated: 'source-structure-job-updated',
  conversationQueueUpdated: 'conversation-queue-updated',
} as const;

export type SSEEventName = (typeof SSE_EVENTS)[keyof typeof SSE_EVENTS];

// localStorage keys — kept in one place to avoid typo drift across slices.
export const STORAGE_KEYS = {
  uiSidebarCollapsed: 'lll.ui.sidebarCollapsed',
  projectSelectedId: 'lll.project.selectedId',
} as const;
