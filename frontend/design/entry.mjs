async function registerWorker() {
  if (!navigator.serviceWorker) throw new Error('请通过本机预览地址或 HTTPS 打开。');
  const revision = document.querySelector('meta[name="lll-design-revision"]')?.content;
  const worker = new URL('/design-worker.mjs', location.origin);
  if (revision) worker.searchParams.set('revision', revision);
  await navigator.serviceWorker.register(worker, { type: 'module', updateViaCache:'none' });
  if (navigator.serviceWorker.controller?.scriptURL === worker.href) return;
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      navigator.serviceWorker.removeEventListener('controllerchange', changed);
      reject(new Error('设计数据更新超时'));
    }, 5000);
    function changed() {
      if (navigator.serviceWorker.controller?.scriptURL !== worker.href) return;
      clearTimeout(timeout);
      navigator.serviceWorker.removeEventListener('controllerchange', changed);
      resolve();
    }
    navigator.serviceWorker.addEventListener('controllerchange', changed);
    changed();
  });
}
async function start() {
  try {
    await registerWorker();
  } catch {
    // Embedded browsers may prohibit service workers. Use the same request handlers in this tab.
    const adapter = '/__design/page-adapter.mjs';
    const {installPageAdapter} = await import(/* @vite-ignore */ adapter);
    installPageAdapter();
  }
  if (import.meta.hot) import.meta.hot.on('design-data-changed', () => location.reload());
  // The product requests a native absolute path. A browser preview only needs a display name.
  const originalFetch = window.fetch.bind(window);
  window.lllOpenExternal = async url => {
    if (![
      'https://auth.openai.com/design-preview',
      'https://github.com/login/oauth/authorize?client_id=lll-design-preview',
      'https://accounts.google.com/o/oauth2/v2/auth?client_id=preview.apps.googleusercontent.com',
    ].includes(url)) throw new Error('设计预览不会打开外部授权页面');
  };
  window.fetch = async (input, init) => {
    if (typeof input === 'string' && input === '/api/workspace/directories' && init?.body && JSON.parse(init.body).pick) {
      try {
        const folder = await chooseFolder();
        if (!folder) return Response.json({cancelled:true});
        const path = '/设计预览/' + folder;
        return Response.json({path, destination:path + '/Learn-With-AI'});
      } catch (err) {
        if (err.name === 'AbortError') return Response.json({cancelled:true});
        return Response.json({error:'无法打开文件夹选择器'}, {status:400});
      }
    }
    return originalFetch(input, init);
  };
  window.lllCaptureRegion = async ({id, width, height}) => {
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(width)); canvas.height = Math.max(1, Math.round(height));
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = getComputedStyle(document.body).backgroundColor;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = getComputedStyle(document.body).color;
    ctx.font = '16px sans-serif'; ctx.fillText('设计预览 · 截图示例', 12, 28);
    window.dispatchEvent(new CustomEvent('lll-region-capture-result', {detail:{id,width,height,dataUrl:canvas.toDataURL()}}));
  };
  if (location.pathname === '/' && !location.search && !localStorage.getItem('lll.design.starter-opened')) {
    localStorage.setItem('lll.design.starter-opened', '1');
    history.replaceState(null, '', '/project/learning-methodology');
  }
  await import('../src/app/main.tsx');
  const anchor = location.hash.slice(1);
  if (anchor) {
    const reveal = () => {
      const target = document.getElementById(anchor);
      if (target) target.scrollIntoView({block:'center'});
      return Boolean(target);
    };
    if (!reveal()) {
      const observer = new MutationObserver(() => { if (reveal()) observer.disconnect(); });
      observer.observe(document.getElementById('root'), {childList:true, subtree:true});
      setTimeout(() => observer.disconnect(), 10000);
    }
  }
}
function chooseFolder() {
  if (window.showDirectoryPicker) return window.showDirectoryPicker().then(handle => handle.name);
  return new Promise(resolve => {
    const input = document.createElement('input');
    input.type = 'file'; input.webkitdirectory = true;
    input.onchange = () => resolve(input.files[0]?.webkitRelativePath.split('/')[0]);
    input.oncancel = () => resolve(null);
    input.click();
  });
}
start().catch(error => { document.getElementById('root').textContent = '设计预览启动失败。' + error.message; });
