// UI slice — transient UI state shared across pages (sidebar collapse,
// drawer open/close, modal open/close). Persisted to localStorage so a
// page refresh keeps the user's chosen layout.

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

import { STORAGE_KEYS } from '@/shared/lib/constants';

interface UiState {
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  summaryPanelCollapsed: boolean;
  drawerOpen: boolean;
  createProjectModalOpen: boolean;
  collapsedFolderIds: string[];
  setSidebarCollapsed: (v: boolean) => void;
  setSidebarWidth: (v: number) => void;
  setSummaryPanelCollapsed: (v: boolean) => void;
  setDrawerOpen: (v: boolean) => void;
  toggleDrawer: () => void;
  openCreateProjectModal: () => void;
  closeCreateProjectModal: () => void;
  toggleFolderCollapsed: (id: string) => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      sidebarWidth: 280,
      summaryPanelCollapsed: false,
      drawerOpen: false,
      createProjectModalOpen: false,
      collapsedFolderIds: [],
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      setSidebarWidth: (v) => set({ sidebarWidth: Math.max(220, Math.min(440, Math.round(v))) }),
      setSummaryPanelCollapsed: (v) => set({ summaryPanelCollapsed: v }),
      setDrawerOpen: (v) => set({ drawerOpen: v }),
      toggleDrawer: () => set((s) => ({ drawerOpen: !s.drawerOpen })),
      openCreateProjectModal: () => set({ createProjectModalOpen: true }),
      closeCreateProjectModal: () => set({ createProjectModalOpen: false }),
      toggleFolderCollapsed: (id) =>
        set((s) => ({
          collapsedFolderIds: s.collapsedFolderIds.includes(id)
            ? s.collapsedFolderIds.filter((x) => x !== id)
            : [...s.collapsedFolderIds, id],
        })),
    }),
    {
      name: STORAGE_KEYS.uiSidebarCollapsed,
      // Only the sidebarCollapsed field is worth persisting across sessions;
      // modals and drawers should always start closed.
      partialize: (s) => ({
        sidebarCollapsed: s.sidebarCollapsed,
        sidebarWidth: s.sidebarWidth,
        summaryPanelCollapsed: s.summaryPanelCollapsed,
        collapsedFolderIds: s.collapsedFolderIds,
      }),
    },
  ),
);
