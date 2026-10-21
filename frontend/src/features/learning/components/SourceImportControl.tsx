import { useRef, useState, type ChangeEvent } from 'react';
import { MAX_SOURCE_FILE_BYTES, useUploadSource } from '../api/learningWorkspace';
import s from './SourceImportControl.module.css';

const extensions = ['pdf', 'md', 'markdown', 'txt', 'pptx', 'docx', 'xlsx', 'csv'];
const fileTypes = extensions.map(extension => `.${extension}`).join(',');
interface ImportResult { name: string; message: string; failed: boolean }

export function SourceImportControl({ slug, compact = false }: { slug: string; compact?: boolean }) {
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
          const response = await upload.mutateAsync({ file });
          result = { name: file.name, failed: false, message: response.source.status === 'ready' ? '已保存并提取文字，请在附件页查看提取范围' : '已保存，正在自动解析' };
        } catch (error) {
          result = { name: file.name, failed: true, message: error instanceof Error ? error.message : '导入失败，请重试' };
        }
      }
      completed.push(result);
      setResults([...completed]);
    }
    setProgress('');
    setBusy(false);
  };

  const failed = results.filter(result => result.failed).length;
  const saved = results.length - failed;
  const summary = [saved ? `已保存 ${saved} 份` : '', failed ? `${failed} 份导入失败` : ''].filter(Boolean).join(' · ');

  return <div className={`${s.control} ${compact ? s.compact : ''}`}>
    <button type="button" className={s.importButton} disabled={busy} onClick={() => input.current?.click()} title="本地自动解析 PDF、Markdown、TXT、PPTX、DOCX、XLSX、CSV；每份不超过 100 MiB。旧版 DOC 请另存为 DOCX。">
      {busy ? '正在导入…' : '上传附件'}
    </button>
    <input ref={input} type="file" accept={fileTypes} multiple hidden onChange={event => void choose(event)} aria-label="选择要导入的资料" />
    {busy ? <span className={s.progress} role="status" title={progress}>{progress}</span> : results.length > 0 && <details className={s.feedback}>
      <summary className={failed ? s.failed : ''}><span role="status">{summary}</span></summary>
      <div className={s.resultPanel}>
        <ul>{results.map((result, index) => <li key={index} className={result.failed ? s.failed : ''}><strong>{result.name}</strong><span>{result.message}</span></li>)}</ul>
        {saved > 0 && <p>解析进度和结果可在「附件」页查看。</p>}
      </div>
    </details>}
  </div>;
}
