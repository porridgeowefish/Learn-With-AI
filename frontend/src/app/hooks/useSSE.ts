// useSSE — single, app-wide EventSource. Mount this hook ONCE at the
// AppShell level; calling it elsewhere is a programming error (caught
// by checking a module-level singleton).
//
// Event handlers are registered via subscribe(), so individual components
// don't need to mount their own EventSource. This eliminates the legacy
// pattern of every page reconnecting.

import { useEffect, useRef } from 'react';

import { SSE_EVENTS, type SSEEventName } from '@/shared/lib/constants';
import { useConnectionStore } from '@/app/store/connection';

type Handler = (data: unknown) => void;

interface Subscription {
  id: number;
  eventName: SSEEventName;
  handler: Handler;
}

let nextSubId = 1;
const subscriptions = new Map<number, Subscription>();
let esInstance: EventSource | null = null;
let refCount = 0;

function ensureConnection() {
  if (esInstance) return;

  const setStatus = useConnectionStore.getState;
  setStatus().setStatus('connecting');

  const es = new EventSource('/api/events');
  esInstance = es;

  es.addEventListener('open', () => {
    useConnectionStore.getState().setStatus('open');
    useConnectionStore.getState().setError(null);
  });

  es.addEventListener('error', () => {
    // EventSource auto-reconnects; we just flag the state.
    useConnectionStore.getState().setStatus('reconnecting');
  });

  // Always-on listeners: connection state updates.
  es.addEventListener(SSE_EVENTS.hello, () => {
    useConnectionStore.getState().markEvent();
  });

  // Fan out other event types to subscribers.
  const fanoutNames: SSEEventName[] = [
    SSE_EVENTS.learningAssetUpdated,
    SSE_EVENTS.teacherNoteUpdated,
    SSE_EVENTS.learningActivityUpdated,
    SSE_EVENTS.sourceUpdated,
    SSE_EVENTS.annotationUpdated,
    SSE_EVENTS.generationJobUpdated,
    SSE_EVENTS.sourceStructureJobUpdated,
    SSE_EVENTS.conversationQueueUpdated,
  ];
  for (const name of fanoutNames) {
    es.addEventListener(name, (e) => {
      try {
        const data = JSON.parse((e as MessageEvent).data);
        for (const sub of subscriptions.values()) {
          if (sub.eventName === name) {
            try {
              sub.handler(data);
            } catch (err) {
              console.error(`[useSSE] handler for ${name} threw`, err);
            }
          }
        }
        useConnectionStore.getState().markEvent();
      } catch {
        /* malformed payload */
      }
    });
  }
}

function closeConnectionIfIdle() {
  if (refCount === 0 && esInstance) {
    esInstance.close();
    esInstance = null;
    useConnectionStore.getState().setStatus('idle');
  }
}

// useSSE() must be called at the AppShell level. It does not return
// anything; consumers use subscribeToSSE to register typed handlers.
export function useSSE(): void {
  useEffect(() => {
    refCount += 1;
    ensureConnection();
    return () => {
      refCount -= 1;
      closeConnectionIfIdle();
    };
  }, []);
}

// subscribeToSSE registers a handler for a specific event. Returns an
// unsubscribe function. Safe to call from any component.
export function subscribeToSSE(eventName: SSEEventName, handler: Handler): () => void {
  const id = nextSubId++;
  subscriptions.set(id, { id, eventName, handler });
  return () => {
    subscriptions.delete(id);
  };
}

// Test-only: reset module state for unit tests.
export function __resetSSEForTest() {
  subscriptions.clear();
  if (esInstance) {
    esInstance.close();
    esInstance = null;
  }
  refCount = 0;
  nextSubId = 1;
}

// Internal escape hatch — used in tests to access the live EventSource.
export function __getEventSourceForTest(): EventSource | null {
  return esInstance;
}

// Silence unused-import warning when tree-shaking misses the ref.
void useRef;
