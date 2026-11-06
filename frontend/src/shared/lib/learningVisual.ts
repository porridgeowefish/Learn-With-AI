// Only provider identifiers cross this boundary. Remote URLs never become img
// sources; the backend resolves the identifier against fixed public hosts.
export interface LearningFigure {
  provider: 'commons' | 'pubchem';
  asset: string;
  title: string;
  sourceUrl: string;
  author: string;
  license: string;
  licenseUrl?: string;
}

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!));
function publicLink(raw: unknown, hosts: string[]): string | null {
  if (typeof raw !== 'string' || raw.length > 1200) return null;
  try { const u = new URL(raw); return u.protocol === 'https:' && !u.username && !u.password && hosts.includes(u.host) ? u.href : null; } catch { return null; }
}

function validAsset(provider: unknown, asset: unknown): boolean {
  return typeof asset === 'string' && asset.length <= 600 && ((provider === 'commons' && /^[a-f0-9]\/[a-f0-9]{2}\/[^/\\?#]+\.svg$/.test(asset)) || (provider === 'pubchem' && /^[1-9][0-9]{0,11}$/.test(asset)));
}

export function renderLearningFigure(source: string): string {
  try {
    if (source.length > 6000) throw new Error('oversized figure');
    const figure = JSON.parse(source) as LearningFigure;
    if (!figure || !validAsset(figure.provider, figure.asset)) throw new Error('invalid asset');
    // The model identifies a candidate; attribution is obtained independently.
    return `<figure class="learning-figure" data-learning-figure="true" data-figure-provider="${escapeHTML(figure.provider)}" data-figure-asset="${escapeHTML(figure.asset)}"><figcaption>正在核对图示来源…</figcaption></figure>`;
  } catch { return '<p class="learning-figure-error">图示信息不完整，暂时无法展示。</p>'; }
}

export function renderVerifiedFigure(source: string): string {
  try {
    if (source.length > 6000) throw new Error('oversized figure');
    const figure = JSON.parse(source) as LearningFigure;
    if (!figure || typeof figure.asset !== 'string' || figure.asset.length > 600) throw new Error('invalid asset');
    const commons = figure.provider === 'commons' && /^[a-f0-9]\/[a-f0-9]{2}\/[^/\\?#]+\.svg$/.test(figure.asset);
    const molecule = figure.provider === 'pubchem' && /^[1-9][0-9]{0,11}$/.test(figure.asset);
    if (!commons && !molecule) throw new Error('invalid provider');
    const sourceURL = publicLink(figure.sourceUrl, [commons ? 'commons.wikimedia.org' : 'pubchem.ncbi.nlm.nih.gov']);
    if (!sourceURL || ![figure.title, figure.author, figure.license].every((v) => typeof v === 'string' && v.length <= 500 && v.trim())) throw new Error('missing attribution');
    const licenseURL = publicLink(figure.licenseUrl, ['creativecommons.org', 'opensource.org']);
    const src = `/api/learning/visuals/image?${new URLSearchParams({ provider: figure.provider, asset: figure.asset })}`;
    const license = licenseURL ? `<a href="${escapeHTML(licenseURL)}" target="_blank" rel="noopener noreferrer">${escapeHTML(figure.license)}</a>` : escapeHTML(figure.license);
    return `<figure class="learning-figure" data-learning-figure="true"><img src="${escapeHTML(src)}" alt="${escapeHTML(figure.title)}" loading="lazy" referrerpolicy="no-referrer"><figcaption><strong>${escapeHTML(figure.title)}</strong><br>${escapeHTML(figure.author)} · ${license} · <a href="${escapeHTML(sourceURL)}" target="_blank" rel="noopener noreferrer">查看来源</a><span class="learning-figure-unavailable" hidden>图示暂时无法加载，可打开来源查看。</span></figcaption></figure>`;
  } catch { return '<p class="learning-figure-error">图示信息不完整，暂时无法展示。</p>'; }
}

export function mountLearningFigures(host: HTMLElement): () => void {
  const cleanups: Array<() => void> = [];
  const controller = new AbortController();
  let disposed = false;
  host.querySelectorAll<HTMLElement>('[data-learning-figure]').forEach((holder) => {
    const provider = holder.dataset.figureProvider;
    const asset = holder.dataset.figureAsset;
    if (!validAsset(provider, asset)) return;
    const endpoint = `/api/learning/visuals/metadata?${new URLSearchParams({provider: provider!, asset: asset!})}`;
    void fetch(endpoint, {signal: controller.signal}).then(async (response) => {
      if (!response.ok) throw new Error('metadata unavailable');
      const figure = await response.json() as LearningFigure;
      if (disposed) return;
      if (figure.provider !== provider || figure.asset !== asset) throw new Error('figure mismatch');
      const markup = document.createElement('div');
      markup.innerHTML = renderVerifiedFigure(JSON.stringify(figure));
      const verified = markup.querySelector('figure');
      if (!verified) throw new Error('invalid attribution');
      holder.replaceChildren(...Array.from(verified.childNodes));
      const img = holder.querySelector('img')!;
      const fail = () => { img.hidden = true; const notice = holder.querySelector<HTMLElement>('.learning-figure-unavailable'); if (notice) notice.hidden = false; };
      img.addEventListener('error', fail);
      if (img.complete && !img.naturalWidth) fail();
      cleanups.push(() => img.removeEventListener('error', fail));
    }).catch(() => {
      if (disposed) return;
      holder.textContent = '暂时无法核实图示来源，未展示图片。';
    });
  });
  return () => { disposed = true; controller.abort(); cleanups.forEach((cleanup) => cleanup()); };
}
