import { normalizeManagedProviderId } from '@/features/cloud-account';

export const conversationModelKey = (slug: string) => `lll.teacher.provider.${slug}`;

export function readConversationModel(slug: string) {
  return normalizeManagedProviderId(localStorage.getItem(conversationModelKey(slug)) ?? '');
}
