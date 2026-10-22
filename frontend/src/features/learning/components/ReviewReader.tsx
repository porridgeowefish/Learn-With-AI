import { useEffect, useMemo, useRef, useState } from 'react';
import { ChevronLeftIcon, ChevronRightIcon } from '@radix-ui/react-icons';
import type { ReactNode } from 'react';

import { BodyAnnotations } from './BodyAnnotations';
import readerStyles from './ReviewReader.module.css';
import assetStyles from './AssetsView.module.css';

interface BodyPage {
  title: string;
}

export function ReviewReader({ slug, content, assetVersionId, renderBody }: { slug: string; content: string; assetVersionId: string; renderBody?: (pageIndex: number) => ReactNode }) {
  const pages = useMemo(() => bodyPages(content), [content]);
  const [pageIndex, setPageIndex] = useState(0);
  const pageTabRefs = useRef(new Map<number, HTMLButtonElement>());
  const currentIndex = Math.min(pageIndex, pages.length - 1);

  useEffect(() => setPageIndex(0), [assetVersionId]);
  useEffect(() => {
    const tab = pageTabRefs.current.get(currentIndex);
    if (tab && typeof tab.scrollIntoView === 'function') {
      tab.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
    }
  }, [currentIndex]);

  const movePage = (offset: number) => setPageIndex((current) => Math.min(Math.max(current + offset, 0), pages.length - 1));

  return <div className={`${readerStyles.layout} ${assetStyles.bodyPager}`}>
    <header className={readerStyles.toc}>
      <div className={readerStyles.tocTitle}>{pages[currentIndex].title}</div>
      <div className={readerStyles.tocNavigation}>
        <button type="button" className={readerStyles.slideButton} onClick={() => movePage(-1)} disabled={currentIndex === 0} aria-label="上一页">
          <ChevronLeftIcon aria-hidden="true" />
        </button>
        <div className={readerStyles.tocScroller} role="tablist" aria-label="复习稿页面" onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') { event.preventDefault(); movePage(-1); }
          if (event.key === 'ArrowRight') { event.preventDefault(); movePage(1); }
        }}>
          {pages.map((page, index) => <button
            key={`${index}-${page.title}`}
            ref={(node) => { if (node) pageTabRefs.current.set(index, node); else pageTabRefs.current.delete(index); }}
            type="button"
            role="tab"
            aria-selected={index === currentIndex}
            aria-label={page.title}
            title={page.title}
            className={`${readerStyles.tocItem} ${index === currentIndex ? readerStyles.active : ''}`}
            onClick={() => setPageIndex(index)}
          ><span className={readerStyles.pageNode}>{index + 1}</span></button>)}
        </div>
        <button type="button" className={readerStyles.slideButton} onClick={() => movePage(1)} disabled={currentIndex === pages.length - 1} aria-label="下一页">
          <ChevronRightIcon aria-hidden="true" />
        </button>
      </div>
    </header>
    <main className={`${readerStyles.reader} ${assetStyles.bodyPage}`}>
      {renderBody ? renderBody(currentIndex) : <BodyAnnotations slug={slug} content={content} assetVersionId={assetVersionId} pageIndex={currentIndex} />}
    </main>
  </div>;
}

function bodyPages(content: string): BodyPage[] {
  const pages: BodyPage[] = [];
  let fence = '';
  for (const line of content.split(/\r?\n/)) {
    const fenceMatch = line.match(/^\s*(`{3,}|~{3,})/);
    if (fenceMatch) {
      if (!fence) fence = fenceMatch[1][0];
      else if (fence === fenceMatch[1][0]) fence = '';
      continue;
    }
    if (fence) continue;
    const heading = line.match(/^##(?!#)\s+(.+?)\s*$/);
    if (heading) pages.push({ title: cleanHeading(heading[1]) });
  }
  return pages.length ? pages : [{ title: '复习稿' }];
}

function cleanHeading(source: string) {
  return source.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1').replace(/[*_`~]/g, '').trim();
}
