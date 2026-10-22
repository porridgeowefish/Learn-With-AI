// Store barrel. Each slice is its own Zustand store; there's no combined
// reducer. Cross-slice interactions happen via selectors in components or
// via the SSE hook dispatching into multiple slices at once.

export { useUiStore } from './slices/ui';
export { useProjectStore } from './slices/project';
export { useConnectionStore } from '@/app/store/connection';
export type { ConnectionStatus } from '@/app/store/connection';
