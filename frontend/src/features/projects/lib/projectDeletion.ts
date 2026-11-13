import { assetViewKey, conversationEffortKey, conversationModelKey } from '@/shared/lib/projectStorage';

// Project-local browser preferences and drafts outlive the server directory.
export function clearDeletedProjectClientData(slug: string): void {
  try {
    localStorage.removeItem(assetViewKey(slug));
    localStorage.removeItem(conversationModelKey(slug));
    localStorage.removeItem(conversationEffortKey(slug));
    localStorage.removeItem(`lll.draft.practice.${slug}`);
    localStorage.removeItem(`lll.practice.${slug}.lastAttempt`);
    const markdownDraftPrefix = `lll.draft.${slug}.`;
    for (let index = localStorage.length - 1; index >= 0; index -= 1) {
      const key = localStorage.key(index);
      if (key?.startsWith(markdownDraftPrefix)) {
        localStorage.removeItem(key);
      }
    }
  } catch {
    // Storage may be unavailable in privacy-restricted browser contexts.
  }
}
