import { useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { TrashIcon } from '@radix-ui/react-icons';
import { Modal } from '@/shared/primitive/Modal';
import type { PreviewSelection } from '@/shared/preview/protocol';
import { useBodyAnnotations, useCreateBodyAnnotation, useDeleteBodyAnnotation, useReviewSearch } from '../api/learningWorkspace';
import { ReviewDocument } from './ReviewDocument';
import s from './BodyAnnotations.module.css';

export function BodyAnnotations({ slug, content, assetVersionId }: { slug: string; content: string; assetVersionId: string }) {
  const [selection, setSelection] = useState<PreviewSelection | null>(null);
  const [clearSelection, setClearSelection] = useState(0);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const annotations = useBodyAnnotations(slug);
  const create = useCreateBodyAnnotation(slug);
  const remove = useDeleteBodyAnnotation(slug);
  const search = useReviewSearch(slug);
  const saved = annotations.data ?? [];

  const highlights = useMemo(() => (annotations.data ?? []).filter(item => item.assetVersionId === assetVersionId).map(item => ({
      id: item.annotationId,
      charStart: item.anchors.start,
      charEnd: item.anchors.end,
      quoteSnapshot: item.quoteSnapshot,
  })), [annotations.data, assetVersionId]);

  const mark = () => {
    if (!selection) return;
    const { start, end, text, prefix, suffix } = selection;
    create.mutate({ assetVersionId, quoteSnapshot: text, anchors: { start, end, prefix, suffix } }, {
      onSuccess: () => {
        setClearSelection(count => count + 1);
        setSelection(null);
      },
    });
  };

  const find = () => {
    if (!selection) return;
    const query = selection.text.replace(/\s+/g, ' ');
    setSearchQuery(query);
    setSelection(null);
    setClearSelection(count => count + 1);
    search.mutate(query);
  };

  return <section aria-label="复习稿正文">
    <div className={s.readerActions}>
      <button type="button" onClick={() => setDrawerOpen(true)}>批注 · {saved.length}</button>
    </div>
    <ReviewDocument content={content} highlights={highlights} clearSelection={clearSelection} onSelection={setSelection} />
    {create.isError && <p role="alert">批注保存失败。请刷新复习稿后重试。</p>}
    {selection && createPortal(
      <div className={s.selectionBar} style={{ top: Math.min(selection.rect.bottom + 8, window.innerHeight - 44), left: Math.max(95, Math.min(window.innerWidth - 95, selection.rect.left + selection.rect.width / 2)) }} role="toolbar" aria-label="选中文本操作">
        <button type="button" onClick={mark} disabled={create.isPending || Array.from(selection.text).length > 1000}>保存批注</button>
        <button type="button" onClick={find} disabled={Array.from(selection.text).length > 256}>元宝搜索</button>
      </div>, document.body,
    )}
    <Modal open={drawerOpen} onOpenChange={setDrawerOpen} title="正文批注">
      <aside className={s.panel} aria-label="正文批注">
        <p className={s.hint}>选中复习稿正文即可保存批注。</p>
        {annotations.isLoading && <p role="status">加载中…</p>}
        {annotations.isError && <p role="alert">批注读取失败。</p>}
        {remove.isError && <p role="alert">批注删除失败。</p>}
        {annotations.isSuccess && saved.length === 0 && <p className={s.empty}>还没有批注。</p>}
        {saved.map(item => <article key={item.annotationId} className={s.item}>
          <blockquote>{item.quoteSnapshot}</blockquote>
          {item.assetVersionId !== assetVersionId && <span className={s.hint}>来自旧版复习稿</span>}
          <button type="button" aria-label="删除批注" onClick={() => remove.mutate(item.annotationId)}><TrashIcon /></button>
        </article>)}
      </aside>
    </Modal>
    <Modal open={Boolean(searchQuery)} onOpenChange={open => { if (!open) setSearchQuery(''); }} title="元宝搜索" description={searchQuery}>
      {search.isPending && <p role="status">正在搜索…</p>}
      {search.isError && <p role="alert">{search.error.message}</p>}
      {search.isSuccess && !search.data.results?.length && <p>没有找到相关结果。</p>}
      <ul className={s.searchResults}>{search.data?.results?.map((item, index) => <li key={index}>
        {/^https?:\/\//i.test(item.url) ? <a href={item.url} target="_blank" rel="noreferrer">{item.title || item.url}</a> : <strong>{item.title}</strong>}
        {item.media && <small>{item.media}</small>}
        {item.snippet && <p>{item.snippet}</p>}
      </li>)}</ul>
    </Modal>
  </section>;
}
