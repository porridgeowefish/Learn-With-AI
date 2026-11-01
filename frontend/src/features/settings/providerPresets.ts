import type { AskProvider } from './askAi';
export interface ProviderPreset {
  id: string; name: string; group: '官方' | '聚合 / 中转' | '套餐';
  kind: AskProvider['kind']; baseURL: string;
  models: readonly { id: string; contextWindowTokens?: number }[];
  docsURL: string; keyURL: string; planURL?: string; note?: string; disabled?: boolean;
}
// Ordinary TokenHub chat models, verified against the official catalog on 2026-10-12.
// Offline models and dedicated translation/media-generation APIs are not teaching models.
const TOKENHUB_MODELS = [
  { id: 'deepseek-v4.1-flash', contextWindowTokens: 1000000 },
  ...['hy4-preview', 'deepseek/deepseek-flash', 'deepseek-v4-flash-202605',
    'deepseek/deepseek-v4-flash-0731', 'deepseek/deepseek-v4-flash', 'deepseek-v4-pro-202606',
    'deepseek/deepseek-v4-pro-0813', 'deepseek/deepseek-v4-pro', 'deepseek/deepseek-v4-flash-vision-exp',
    'deepseek-v4-flash-0731', 'deepseek-v4-pro-0813', 'deepseek-v4-flash', 'deepseek-v4-pro',
    'glm-5.3-flash', 'glm-5.3-flashx', 'glm-5.3', 'glm-5.2', 'kimi-k3', 'kimi-k2.8-preview',
    'minimax-m3', 'mimo-v2.6-pro', 'mimo-v2.6-flash', 'mimo-v2.5-pro', 'step-5-preview']
    .map(id => ({ id, contextWindowTokens: 1000000 })),
  ...['hy3', 'kimi-k2.7-code-highspeed', 'kimi-k2.7-code', 'kimi-k2.6']
    .map(id => ({ id, contextWindowTokens: 256000 })),
  ...['glm-5v-turbo', 'minimax-m2.7'].map(id => ({ id, contextWindowTokens: 200000 })),
  ...['hunyuan-role-latest', 'hy-role'].map(id => ({ id, contextWindowTokens: 32000 })),
];
export const PROVIDER_PRESETS: readonly ProviderPreset[] = [
  { id: 'tencent-personal-plan', name: '腾讯云 · 个人通用 Token Plan', group: '套餐', kind: 'openai', baseURL: 'https://api.lkeap.cloud.tencent.com/plan/v3', models: ['tc-code-latest', 'deepseek/deepseek-flash', 'deepseek-v4-flash-202605', 'deepseek-v4-pro-202606', 'minimax-m2.7', 'minimax-m3', 'glm-5.2', 'glm-5.3', 'glm-5.3-flash', 'hy4-preview', 'kimi-k2.7-code', 'kimi-k3', 'mimo-v2.6-flash'].map(id => ({ id })), docsURL: 'https://cloud.tencent.com/document/product/1823/130119', keyURL: 'https://console.cloud.tencent.com/tokenhub/tokenplan', planURL: 'https://cloud.tencent.com/document/product/1823/130060', note: '个人套餐限官方允许的 AI 工具；自建应用后端受官方使用限制。' },
  { id: 'tencent-personal-anthropic', name: '腾讯云 · 个人通用 Token Plan（Anthropic）', group: '套餐', kind: 'anthropic', baseURL: 'https://api.lkeap.cloud.tencent.com/plan/anthropic', models: ['tc-code-latest', 'deepseek/deepseek-flash', 'deepseek-v4-flash-202605', 'deepseek-v4-pro-202606', 'minimax-m2.7', 'minimax-m3', 'glm-5.2', 'glm-5.3', 'glm-5.3-flash', 'hy4-preview', 'kimi-k2.7-code', 'kimi-k3', 'mimo-v2.6-flash'].map(id => ({ id })), docsURL: 'https://cloud.tencent.com/document/product/1823/130119', keyURL: 'https://console.cloud.tencent.com/tokenhub/tokenplan', planURL: 'https://cloud.tencent.com/document/product/1823/130060', note: '使用个人通用套餐专属 Key；官方用途限制同上。' },
  { id: 'glm-coding', name: '智谱 · GLM Coding Plan', group: '套餐', kind: 'openai', baseURL: 'https://open.bigmodel.cn/api/coding/paas/v4', models: [{ id: 'glm-5.3', contextWindowTokens: 1000000 }, { id: 'glm-5.3-flash' }], docsURL: 'https://zcode.z.ai/cn/docs/configuration', keyURL: 'https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys', planURL: 'https://bigmodel.cn/glm-coding', note: '使用编程套餐 API Key；仅限 Coding 场景，可用模型与额度以账号权限为准。' },
  { id: 'glm-coding-anthropic', name: '智谱 · GLM Coding Plan（Anthropic）', group: '套餐', kind: 'anthropic', baseURL: 'https://open.bigmodel.cn/api/anthropic', models: [{ id: 'glm-5.3', contextWindowTokens: 1000000 }, { id: 'glm-5.3-flash' }], docsURL: 'https://zcode.z.ai/cn/docs/configuration', keyURL: 'https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys', planURL: 'https://bigmodel.cn/glm-coding', note: '使用编程套餐 API Key；仅限 Coding 场景，可用模型与额度以账号权限为准。' },
  { id: 'openai', name: 'OpenAI', group: '官方', kind: 'openai', baseURL: 'https://api.openai.com/v1', models: [{ id: 'chat-latest', contextWindowTokens: 400000 }], docsURL: 'https://developers.openai.com/api/docs/models/chat-latest', keyURL: 'https://platform.openai.com/api-keys' },
  { id: 'tencent-tokenhub', name: '腾讯云 · TokenHub（免费体验 / 普通 API）', group: '官方', kind: 'openai', baseURL: 'https://tokenhub.tencentmaas.com/v1', models: TOKENHUB_MODELS, docsURL: 'https://cloud.tencent.com/document/product/1823/130058', keyURL: 'https://console.cloud.tencent.com/tokenhub/apikey', note: '列表不代表账号权限。请选择控制台已开通的模型。' },
  { id: 'claude', name: 'Claude', group: '官方', kind: 'anthropic', baseURL: 'https://api.anthropic.com', models: [{ id: 'claude-sonnet-5-5' }, { id: 'claude-haiku-5-5' }, { id: 'claude-opus-5-5' }], docsURL: 'https://platform.claude.com/docs/en/models/overview', keyURL: 'https://platform.claude.com/settings/keys' },
  { id: 'gemini', name: 'Google Gemini', group: '官方', kind: 'openai', baseURL: 'https://generativelanguage.googleapis.com/v1beta/openai', models: ['gemini-3.8-flash', 'gemini-3.6-flash', 'gemini-3.5-flash-lite', 'gemini-3.1-flash-lite', 'gemini-3.1-pro-preview', 'gemini-3-flash-preview'].map(id => ({ id })), docsURL: 'https://ai.google.dev/gemini-api/docs/openai', keyURL: 'https://aistudio.google.com/api-keys' },
  { id: 'deepseek', name: 'DeepSeek', group: '官方', kind: 'openai', baseURL: 'https://api.deepseek.com', models: [{ id: 'deepseek-flash' }, { id: 'deepseek-v4-pro' }], docsURL: 'https://api-docs.deepseek.com/', keyURL: 'https://platform.deepseek.com/api_keys' },
  { id: 'qwen', name: '阿里云百炼（中国）', group: '官方', kind: 'openai', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', models: [{ id: 'qwen-plus' }], docsURL: 'https://help.aliyun.com/zh/model-studio/model-calling-in-sub-workspace', keyURL: 'https://bailian.console.aliyun.com/' },
  { id: 'glm', name: '智谱 GLM（普通 API）', group: '官方', kind: 'openai', baseURL: 'https://open.bigmodel.cn/api/paas/v4', models: [{ id: 'glm-5.3', contextWindowTokens: 1000000 }, { id: 'glm-5.3-flash' }], docsURL: 'https://docs.bigmodel.cn/cn/guide/start/model-overview', keyURL: 'https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys' },
  { id: 'xai', name: 'xAI', group: '官方', kind: 'openai', baseURL: 'https://api.x.ai/v1', models: [{ id: 'grok-4.7' }], docsURL: 'https://docs.x.ai/build/settings', keyURL: 'https://console.x.ai/' },
  { id: 'minimax', name: 'MiniMax（中国）', group: '官方', kind: 'openai', baseURL: 'https://api.minimax.cn/v1', models: [{ id: 'MiniMax-M3', contextWindowTokens: 1000000 }, { id: 'MiniMax-M2.7', contextWindowTokens: 204800 }], docsURL: 'https://platform.minimax.cn/docs/api-reference/text-openai-api', keyURL: 'https://platform.minimax.cn/', planURL: 'https://platform.minimax.cn/subscribe/token-plan', note: '普通 Key / Token Plan Key 使用同一入口，按 Key 计费。' },
  { id: 'kimi', name: 'Kimi', group: '官方', kind: 'openai', baseURL: 'https://api.moonshot.cn/v1', models: [{ id: 'kimi-k3', contextWindowTokens: 1000000 }], docsURL: 'https://platform.kimi.com/docs/models', keyURL: 'https://platform.kimi.com/docs/get-api-key' },
  { id: 'minimax-global', name: 'MiniMax（国际）', group: '官方', kind: 'openai', baseURL: 'https://api.minimax.io/v1', models: [{ id: 'MiniMax-M3', contextWindowTokens: 1000000 }, { id: 'MiniMax-M2.7', contextWindowTokens: 204800 }], docsURL: 'https://platform.minimax.io/docs/api-reference/text-openai-api', keyURL: 'https://platform.minimax.io/', planURL: 'https://platform.minimax.io/subscribe/token-plan' },
  { id: 'groq', name: 'Groq', group: '官方', kind: 'openai', baseURL: 'https://api.groq.com/openai/v1', models: [{ id: 'openai/gpt-oss-120b', contextWindowTokens: 131072 }, { id: 'openai/gpt-oss-20b', contextWindowTokens: 131072 }], docsURL: 'https://console.groq.com/docs/openai', keyURL: 'https://console.groq.com/keys' },
  { id: 'together', name: 'Together AI', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://api.together.ai/v1', models: [{ id: 'MiniMaxAI/MiniMax-M3' }], docsURL: 'https://docs.together.ai/docs/inference/openai-compatibility', keyURL: 'https://api.together.ai/' },
  { id: 'deepinfra', name: 'DeepInfra', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://api.deepinfra.com/v1/openai', models: [{ id: 'deepseek-ai/DeepSeek-V4-Flash-0731' }], docsURL: 'https://docs.deepinfra.com/chat/overview', keyURL: 'https://deepinfra.com/' },
  { id: 'aihubmix', name: 'AIHubMix', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://aihubmix.com/v1', models: [{ id: 'gpt-4o-mini' }], docsURL: 'https://docs.aihubmix.com/cn/quick-start', keyURL: 'https://aihubmix.com/' },
  { id: 'siliconflow', name: '硅基流动', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://api.siliconflow.cn/v1', models: [{ id: 'Pro/deepseek-ai/DeepSeek-R1' }, { id: 'Pro/zai-org/GLM-5.1' }], docsURL: 'https://docs.siliconflow.cn/docs/userguide/quickstart', keyURL: 'https://cloud.siliconflow.cn/account/ak' },
  { id: 'openrouter', name: 'OpenRouter', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://openrouter.ai/api/v1', models: [{ id: 'openrouter/auto' }], docsURL: 'https://openrouter.ai/docs/guides/routing/routers/auto-router', keyURL: 'https://openrouter.ai/settings/keys', note: '自动路由按所选模型计费。' },
  { id: '302', name: '302.AI', group: '聚合 / 中转', kind: 'openai', baseURL: 'https://api.302.ai/v1', models: [{ id: 'deepseek/deepseek-r1-0528' }], docsURL: 'https://doc.302.ai/308031416e0', keyURL: 'https://302.ai/' },
];
export const CODING_PLAN_LINKS = [
  { id: 'tencent-token', name: '腾讯云个人通用 Token Plan', docsURL: 'https://cloud.tencent.com/document/product/1823/130060', note: '使用专属地址与模型，按官方允许用途使用。' },
  { id: 'glm-coding', name: 'GLM Coding Plan', docsURL: 'https://zcode.z.ai/cn/docs/configuration', note: '可选择 OpenAI / Anthropic 编程套餐入口；模型与额度以账号权限为准。' },
  { id: 'aliyun-coding', name: '阿里云 Coding Plan', docsURL: 'https://help.aliyun.com/zh/model-studio/coding-plan', note: '不支持自建 API 后端。' },
  { id: 'volc-coding', name: '火山引擎 Coding Plan', docsURL: 'https://docs.volcengine.com/docs/ark/coding-plan-personal-plan-overview?lang=zh', note: '仅用于支持的编程客户端。' },
] as const;
const normalizedURL = (url: string) => url.replace(/\/+$/, '');
// Match all preset-owned values conservatively. Unknown/custom limits stay editable.
export function presetForProvider(provider: AskProvider): ProviderPreset | undefined {
  return PROVIDER_PRESETS.find(preset => !preset.disabled && preset.kind === provider.kind
    && normalizedURL(preset.baseURL) === normalizedURL(provider.baseURL)
    && preset.models.some(model => model.id === provider.model
      && (provider.contextWindowTokens === undefined || provider.contextWindowTokens === model.contextWindowTokens)));
}
export function applyProviderPreset(provider: AskProvider, preset: ProviderPreset): AskProvider {
  const model = preset.models[0];
  return { ...provider, name: preset.name, kind: preset.kind, baseURL: preset.baseURL,
    model: model.id, contextWindowTokens: model.contextWindowTokens, reasoning: false, thinking: false };
}
