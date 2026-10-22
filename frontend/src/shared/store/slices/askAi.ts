import { create } from 'zustand';

import type { AskMessage } from '@/features/learning/api/annotations';

export interface LiveAskMessage extends AskMessage {
  thinking?: string; // live-only; not persisted by the backend
}

export interface AskAnchor {
  top: number; // screen coords (position: fixed)
  left: number;
  right?: number;
  bottom?: number;
  width?: number;
  height?: number;
}

export interface AskPanelGeometry {
  top: number;
  left: number;
  width: number;
  height: number;
}

interface AskAiState {
  open: boolean;
  reviewMode: boolean;
  projectSlug: string;
  confusionId: string;
  quote: string;
  anchor: AskAnchor;
  messages: LiveAskMessage[];
  streaming: boolean;
  providerId: string;
  initialInput: string;
  geometry: AskPanelGeometry | null;

  openActive: (args: {
    projectSlug: string;
    confusionId: string;
    quote: string;
    anchor: AskAnchor;
    providerId: string;
    initialInput?: string;
  }) => void;
  openReview: (args: {
    projectSlug: string;
    confusionId: string;
    quote: string;
    anchor: AskAnchor;
    messages: AskMessage[];
  }) => void;
  close: () => void;
  appendUser: (content: string) => void;
  startAssistant: () => void;
  appendDelta: (type: 'text' | 'thinking', content: string) => void;
  finishStream: () => void;
  setProvider: (id: string) => void;
  setGeometry: (geometry: AskPanelGeometry) => void;
}

let msgSeq = 0;
function localId() {
  msgSeq += 1;
  return `live-${msgSeq}-${Date.now()}`;
}

export const useAskAiStore = create<AskAiState>()((set) => ({
  open: false,
  reviewMode: false,
  projectSlug: '',
  confusionId: '',
  quote: '',
  anchor: { top: 0, left: 0 },
  messages: [],
  streaming: false,
  providerId: '',
  initialInput: '',
  geometry: null,

  openActive: ({ projectSlug, confusionId, quote, anchor, providerId, initialInput }) =>
    set({
      open: true,
      reviewMode: false,
      projectSlug,
      confusionId,
      quote,
      anchor,
      providerId,
      initialInput: initialInput ?? '',
      geometry: null,
      messages: [],
      streaming: false,
    }),
  openReview: ({ projectSlug, confusionId, quote, anchor, messages }) =>
    set({
      open: true,
      reviewMode: true,
      projectSlug,
      confusionId,
      quote,
      anchor,
      messages: messages.map((m) => ({ ...m })),
      streaming: false,
      providerId: '',
      initialInput: '',
      geometry: null,
    }),
  close: () => set({ open: false, streaming: false, messages: [], geometry: null, initialInput: '' }),
  appendUser: (content) =>
    set((s) => ({
      messages: [...s.messages, { id: localId(), role: 'user', content, createdAt: new Date().toISOString() }],
    })),
  startAssistant: () =>
    set((s) => ({
      streaming: true,
      messages: [
        ...s.messages,
        { id: localId(), role: 'assistant', content: '', thinking: '', createdAt: new Date().toISOString() },
      ],
    })),
  appendDelta: (type, content) =>
    set((s) => {
      if (s.messages.length === 0) return {};
      const msgs = [...s.messages];
      const last = { ...msgs[msgs.length - 1] };
      if (last.role !== 'assistant') return {};
      if (type === 'thinking') last.thinking = (last.thinking ?? '') + content;
      else last.content = (last.content ?? '') + content;
      msgs[msgs.length - 1] = last;
      return { messages: msgs };
    }),
  finishStream: () => set({ streaming: false }),
  setProvider: (id) => set({ providerId: id }),
  setGeometry: (geometry) => set({ geometry }),
}));
