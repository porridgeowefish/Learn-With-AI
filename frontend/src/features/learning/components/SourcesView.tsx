import { useEffect, useState } from 'react';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import { usePermanentlyDeleteSource, useRetrySource, useSources, type LearningSource } from '@/features/learning/api/learningWorkspace';
import { SourceImportControl } from './SourceImportControl';
import { SourceStructureControl } from './SourceStructureControl';

import s from './SourcesView.module.css';

// Imported materials automatically participate in the next teacher request.
export function SourcesView({ slug }: { slug: string }) {
  const sources = useSources(slug);
  const retry = useRetrySource(slug);
  const remove = usePermanentlyDeleteSource(slug);
  const [selected, setSelected] = useState<LearningSource>();
  useEffect(() => { setSelected(undefined); remove.reset?.(); }, [slug]);
  const visible = (sources.data ?? []).filter((source) => source.status !== 'tombstoned' && source.status !== 'deleted');

  return (
    <section className={s.sources}>
      <header>
        <div><span>附件库</span><h2>当前文件</h2><p>导入后自动在本地提取文本，可供教师读取。支持 PDF、Markdown、TXT、PPTX、DOCX、XLSX、CSV，每份不超过 100 MiB。</p><p>免费识别使用本机 OCR，无需 API Key；一次启动后在后台处理整份资料，离开此页也会继续。可以查看进度或取消，本地识别重启后恢复。模型识别整份资料会逐页调用已配置的文档模型，按服务商计费。已提取正文可直接用于学习，公式和化学结构仍需对照原件核对。旧版 DOC 请另存为 DOCX 后导入。</p></div>
        <SourceImportControl slug={slug} />
      </header>
      {sources.isError && <div className={s.error} role="alert">资料列表读取失败。<button type="button" onClick={() => void sources.refetch()}>重试</button></div>}
      {retry.isError && <div className={s.error} role="alert">重试解析失败：{retry.error.message}</div>}
      <section className={s.librarySection}>
        <div className={s.sectionHeading}><div><span>上传附件</span><h3>文件项</h3></div><small>{visible.length} 份</small></div>
        <div className={s.list}>
          {visible.map((source) => <article key={source.sourceId}>
            <div className={s.fileIcon}>{extension(source.displayName)}</div>
            <div className={s.fileDetails}><strong>{source.displayName}</strong><span className={source.status === 'failed' ? s.failure : ''}>{statusLabel(source)}</span>{source.extractionTruncated && <span className={s.warning}>文本过长，仅提取了部分内容；原文件已保留。</span>}<SourceStructureControl slug={slug} source={source}/></div>
            <div className={s.trailing}><time>{new Date(source.updatedAt).toLocaleString()}</time>{canRetry(source) && <button type="button" className={s.retryButton} disabled={retry.isPending} aria-label={`重试解析 ${source.displayName}`} onClick={() => retry.mutate(source.sourceId)}>{retry.isPending && retry.variables === source.sourceId ? '正在重试…' : '重试解析'}</button>}<button type="button" className={s.danger} aria-label={`删除资料 ${source.displayName}`} onClick={() => { remove.reset?.(); setSelected(source); }}>删除</button></div>
          </article>)}
          {!sources.isLoading && !visible.length && <div className={s.empty}>尚未上传附件</div>}
        </div>
      </section>
      <Modal open={Boolean(selected)} onOpenChange={open => { if (!open && !remove.isPending) setSelected(undefined); }} title="删除资料" description="永久删除这份资料及其解析文字。" footer={<><Button onClick={() => setSelected(undefined)} disabled={remove.isPending}>取消</Button><Button variant="danger" loading={remove.isPending} onClick={() => selected && remove.mutate(selected.sourceId, { onSuccess: () => setSelected(undefined) })}>永久删除</Button></>}>
        <p className={s.deleteName}>{selected?.displayName}</p><p>原件、解析文字、识别页图、历史解析版本与检索缓存将永久删除，无法恢复。</p><p>已生成的地图和学习内容会保留。</p>{remove.isError && <p role="alert" className={s.error}>删除失败：{remove.error.message}</p>}
      </Modal>
    </section>
  );
}

function extension(name: string) {
  const value = name.split('.').pop()?.toUpperCase() || 'FILE';
  return value.slice(0, 5);
}

function statusLabel(source: LearningSource) {
  const failures: Record<string, string> = {
    unsupported_legacy_format: '旧版 DOC 暂不支持，请另存为 DOCX 后重新导入。',
    unsupported_format: '暂不支持此格式，请转换为支持的格式后重新导入。',
    invalid_document: '文件格式无效或已损坏，请检查后重新导入。',
    encrypted_document: '文件已加密，请解密后重新导入。',
    resource_limit: '文件内容超出解析资源限制，请拆分后重新导入。',
    no_extractable_text: '没有可直接提取的文字，请使用“识别扫描页”；重新解析不会识别图片中的文字。',
    parse_cancelled: '解析被中断，可以重试。',
    parse_failed: '解析未完成，可以重试。',
  };
  if (source.status === 'failed' && source.failureCode === 'no_extractable_text') return '扫描资料：没有可直接提取的文字，请识别扫描页。';
  if (source.status === 'failed') return `解析失败：${failures[source.failureCode ?? ''] ?? '请检查文件是否损坏、加密或没有可提取文字，再重新导入。'}`;
  if (source.status === 'ready' && source.coverage?.totalPages) {
    const coverage = source.coverage;
    if (coverage.unreadPages) return `仅提取 ${coverage.extractedPages}/${coverage.totalPages} 页文字 · ${coverage.unreadPages} 页仍需识别`;
    return `已提取 ${coverage.extractedPages}/${coverage.totalPages} 页文字${coverage.complete ? '' : ' · 内容仍需核对'}`;
  }
  if (source.status === 'opaque' && canRetry(source)) return '原文件已保存，可重试本地解析';
  const labels: Record<string, string> = { stored: '已保存，等待解析', processing: '正在准备资料', ready: '已提取文字，完整性未核验', opaque: '已保存，暂不支持解析' };
  return labels[source.status] ?? source.status;
}

function canRetry(source: LearningSource) {
  return (source.status === 'failed' || source.status === 'opaque') && /\.(pdf|md|markdown|txt|pptx|docx|xlsx|csv)$/i.test(source.displayName);
}
