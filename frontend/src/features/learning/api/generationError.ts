import {ApiError} from '@/shared/client';

const messages: Record<string,string> = {
 'provider-unavailable':'所选 API 连接不可用，请检查 Key 和连接配置。',
 'model-unavailable':'未配置有效模型，请选择或填写模型名称。',
 'input-budget-exhausted':'本次资料与对话超过模型上下文预算，请减少资料或调整连接的上下文容量。',
 'generation-active':'此内容正在生成，请查看当前任务进度。',
 'generation-queue-full':'生成队列已满，请稍后重试。',
 'generation-project-type':'此页面不支持这类生成，请从对应学习单元操作。',
 'source-not-readable':'所选资料尚未解析完成，请等待或取消选择该资料。',
 'source-unavailable':'所选资料已不可用，请重新选择。',
 'source-selection-required':'请先选择可用的参考资料。',
 'source-structure-incomplete':'参考资料的章节结构不完整，请补充资料后重试。',
 'source-structure-unavailable':'资料结构暂不可用，请更新应用后根据已提取正文重新生成。',
 'source-verification-unavailable':'所选资料尚无可读正文，请先识别扫描页或重新选择资料。',
 'source-structure-budget':'参考资料的章节目录超出本次处理预算，请拆分资料后生成。',
 'map-version-conflict':'地图已在生成期间更新，当前地图已保留，请重新生成。',
 'map-recovery-required':'地图版本文件不完整，请恢复资料或重新生成地图。',
 'prompt-resource-unavailable':'教学生成资源缺失或版本不一致，请修复或更新安装包。',
 'migration-blocked':'旧任务尚未完成兼容迁移，请先关闭旧版程序后重新启动。',
 'generation-unavailable':'生成服务未就绪，请重新启动应用。',
 'provider-failed':'模型服务调用失败，请检查连接、套餐权限与额度。',
 'output-limit':'提供方返回输出截断。应用单次输出上限为 100,000 token；提供方仍可能有更低限制。未完成内容没有覆盖原稿，可重试或换模型。',
 'empty-output':'模型没有返回正文，请重试或检查模型的思考与输出配置。',
 'invalid-practice-output':'练习题未完整满足题数、答案或解析要求，请减少题数后重试。',
 'invalid-map-output':'模型返回的地图未通过格式或结构校验；旧任务未保存具体原因。重新生成会再次调用模型。',
 'invalid-review-output':'模型未生成完整的 HTML 复习稿，已保留上次结果。可以重试。',
 'edit-conflict':'生成期间原稿已修改，旧稿已保留，请重新生成。',
 'request-interrupted':'生成已中断，可重新发起。',
 'operation-conflict':'本次提交内容已变化，请关闭弹窗后重新生成。',
};
export function describeGenerationError(error: unknown): string {
 if(typeof error==='string')return Object.prototype.hasOwnProperty.call(messages,error)?messages[error]:'生成未完成，请重试或检查连接。';
 if(error instanceof ApiError){
  const body=error.body as {error?: {code?: unknown}}|null;
  const code=typeof body?.error==='object'&&typeof body.error?.code==='string'?body.error.code:'';
  if(Object.prototype.hasOwnProperty.call(messages,code))return messages[code];
  if(error.status===0)return '无法连接本地服务，请确认应用仍在运行后重试。';
  return `提交未完成（HTTP ${error.status}），请重试。`;
 }
 return '提交未完成，请重试。';
}
