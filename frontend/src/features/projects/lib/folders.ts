// Pure helpers + types for the workspace-global project folder layout.
// The layout is a list of named folders, each holding project slugs by
// reference (no files move on disk). A project belongs to at most one folder
// (tree-style membership); any project referenced by no folder is uncategorized.
//
// These functions are pure and never mutate their input — the component builds
// the next layout and PUTs it to the backend, which sanitizes again.

export interface Folder {
  id: string;
  name: string;
  slugOrder: string[];
  /** Discipline-map project rendered as this folder's explicit overview row. */
  mapProjectSlug?: string;
}

export interface FolderLayout {
  folders: Folder[];
}

export const EMPTY_LAYOUT: FolderLayout = { folders: [] };

export function cloneLayout(layout: FolderLayout): FolderLayout {
  return {
    folders: layout.folders.map((f) => ({ ...f, slugOrder: [...f.slugOrder] })),
  };
}

let idCounter = 0;
function genId(): string {
  // Stable enough client-side; the backend re-stamps on collision/missing.
  idCounter += 1;
  return `f_${Date.now().toString(36)}_${idCounter}`;
}

/** Slugs that appear in no folder — the uncategorized projects. */
export function uncategorizedSlugs(layout: FolderLayout, allSlugs: string[]): string[] {
  const assigned = new Set<string>();
  for (const f of layout.folders) for (const sl of f.slugOrder) assigned.add(sl);
  return allSlugs.filter((sl) => !assigned.has(sl));
}

/** Folder id that currently holds the slug, or null if uncategorized. */
export function folderOf(layout: FolderLayout, slug: string): string | null {
  for (const f of layout.folders) {
    if (f.slugOrder.includes(slug)) return f.id;
  }
  return null;
}

/** Move a project into a folder (folderId !== null) or out to uncategorized. */
export function assignProject(
  layout: FolderLayout,
  slug: string,
  folderId: string | null,
): FolderLayout {
  const next = cloneLayout(layout);
  for (const f of next.folders) {
    f.slugOrder = f.slugOrder.filter((sl) => sl !== slug);
  }
  if (folderId !== null) {
    const target = next.folders.find((f) => f.id === folderId);
    if (target) target.slugOrder.push(slug);
  }
  return next;
}

export function createFolder(layout: FolderLayout, name: string): FolderLayout {
  const trimmed = name.trim();
  if (!trimmed) return layout;
  const next = cloneLayout(layout);
  next.folders.push({ id: genId(), name: trimmed, slugOrder: [] });
  return next;
}

export function renameFolder(layout: FolderLayout, id: string, name: string): FolderLayout {
  const trimmed = name.trim();
  if (!trimmed) return layout;
  const next = cloneLayout(layout);
  const f = next.folders.find((x) => x.id === id);
  if (f) f.name = trimmed;
  return next;
}
