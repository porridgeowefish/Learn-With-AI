import { normalizeManagedProviderId } from '@/features/cloud-account';
import { conversationModelKey } from '@/shared/lib/projectStorage';

export function readConversationModel(slug: string) {
  return normalizeManagedProviderId(localStorage.getItem(conversationModelKey(slug)) ?? '');
}
