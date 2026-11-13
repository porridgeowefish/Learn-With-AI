import { useRef, useState, type ChangeEvent } from 'react';
import { MAX_SOURCE_FILE_BYTES, useUploadSource } from '../api/learningWorkspace';
import s from './SourceImportControl.module.css';

const extensions = ['pdf', 'md', 'markdown', 'txt', 'pptx', 'docx', 'xlsx', 'csv'];
const fileTypes = extensions.map(extension => `.${extension}`).join(',');
interface ImportResult { name: string; message: string; failed: boolean }

export function SourceImportControl({ slug, compact = false, onBusyChange }: { slug: string; compact?: boolean; onBusyChange?: (busy: boolean) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const upload = useUploadSource(slug);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState('');
  const [results, setResults] = useState<ImportResult[]>([]);

  const choose = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = '';
    if (!files.length || busy) return;
    setBusy(true);
    onBusyChange?.(true);
    setResults([]);
    const completed: ImportResult[] = [];
    for (const [index, file] of files.entries()) {
      setProgress(`正在导入 ${index + 1}/${files.length}：${file.name}`);
      const extension = file.name.split('.').pop()?.toLowerCase();
      let result: ImportResult;
      if (!extensions.includes(extension ?? '')) {
        result = { name: file.name, failed: true, message: extension === 'doc' ? '请另存为 DOCX 后导入' : '暂不支持此格式' };
      } else if (file.size > MAX_SOURCE_FILE_BYTES) {
        result = { name: file.name, failed: true, message: '文件超过 100 MiB，请拆分后导入' };
      } else {
        try {
          const response = await upload.mutateAsync({ file, operationId: crypto.randomUUID() });
          result = { name: file.name, failed: false, message: response.source.status === 'ready' ? '已保存，文字已可读取；扫描页会继续自动识别' : '已保存，正在后台解析整份资料' };
        } catch (error) {
          result = { name: file.name, failed: true, message: error instanceof Error ? error.message : '导入失败，请重试' };
        }
      }
      completed.push(result);
      setResults([...completed]);
    }
    setProgress('');
    setBusy(false);
    onBusyChange?.(false);
  };

  const failed = results.filter(result => result.failed).length;
  const saved = results.length - failed;
  const summary = [saved ? `已保存 ${saved} 份` : '', failed ? `${failed} 份导入失败` : ''].filter(Boolean).join(' · ');

  return <div data-tour="source-upload" className={`${s.control} ${compact ? s.compact : ''}`}>
    <button type="button" className={s.importButton} disabled={busy} onClick={() => input.current?.click()} title="本地自动解析 PDF、Markdown、TXT、PPTX、DOCX、XLSX、CSV；每份不超过 100 MiB。旧版 DOC 请另存为 DOCX。">
      {busy ? '正在导入…' : '上传附件'}
    </button>
    <input ref={input} type="file" accept={fileTypes} multiple hidden onChange={event => void choose(event)} aria-label="选择要导入的资料" />
    {busy ? <span className={s.progress} role="status" title={progress}>{progress}</span> : results.length > 0 && <details className={s.feedback}>
      <summary className={failed ? s.failed : ''}><span role="status">{summary}</span></summary>
      <div className={s.resultPanel}>
        <ul>{results.map((result, index) => <li key={index} className={result.failed ? s.failed : ''}><strong>{result.name}</strong><span>{result.message}</span></li>)}</ul>
        {saved > 0 && <p>无需再次点击解析。扫描页自动在本机逐页识别，进度可在「附件」查看；教师按问题读取所需内容。</p>}
      </div>
    </details>}
  </div>;
}
