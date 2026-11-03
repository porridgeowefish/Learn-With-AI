import { dispatch, subscribeEvents } from '/__design/runtime.mjs';

function isDesignRequest(url) {
  return url.origin === location.origin && /^\/(api|files|events)(\/|$)/.test(url.pathname);
}
export function installPageAdapter() {
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : input, location.href);
    if (!isDesignRequest(url)) return nativeFetch(input, init);
    const request = new Request(input instanceof Request ? input : url, init);
    try { return await dispatch(request, url); }
    catch (e) { return Response.json({error:String(e.message || e)}, {status:500}); }
  };

  window.EventSource = class extends EventTarget {
    constructor(url) {
      super();
      if (new URL(url, location.href).pathname !== '/api/events') throw new Error('预览事件路径未登记');
      this.readyState = 0;
      this.unsubscribe = subscribeEvents((name, value) => this.dispatchEvent(new MessageEvent(name,{data:JSON.stringify(value)})));
      queueMicrotask(() => {
        if (this.readyState !== 0) return;
        this.readyState = 1; this.dispatchEvent(new Event('open'));
        this.dispatchEvent(new MessageEvent('hello',{data:'{}'}));
      });
    }
    close() { this.readyState = 2; this.unsubscribe(); }
  };

  // Native HTML downloads and media normally go through the worker as well.
  // Convert only mock-backed URLs to blobs when that worker is unavailable.
  const resources = new WeakMap();
  const resolveResource = async el => {
    const attr = el.matches('a') ? 'href' : 'src';
    const raw = el.getAttribute(attr);
    if (!raw || !isDesignRequest(new URL(raw,location.href))) return;
    const previous = resources.get(el);
    if (previous?.raw === raw) return;
    if (previous?.blob) URL.revokeObjectURL(previous.blob);
    const item = {raw}; resources.set(el,item);
    const response = await window.fetch(raw);
    if (!response.ok || !el.isConnected || el.getAttribute(attr)!==raw) return;
    item.blob = URL.createObjectURL(await response.blob());
    if (el.matches('a[download]') && !el.download) el.download = '学习对话.md';
    el.setAttribute(attr,item.blob);
  };
  const eachResource = (node, fn) => {
    if (!(node instanceof Element)) return;
    const selector='a[href],img[src],audio[src],video[src],source[src]';
    if (node.matches(selector)) fn(node);
    node.querySelectorAll(selector).forEach(fn);
  };
  new MutationObserver(records => {
    for (const record of records) {
      if (record.type==='attributes') void resolveResource(record.target);
      record.addedNodes.forEach(node=>eachResource(node,el=>void resolveResource(el)));
      record.removedNodes.forEach(node=>eachResource(node,el=>{
        const item=resources.get(el);
        if (!el.isConnected && item) { if(item.blob) URL.revokeObjectURL(item.blob);resources.delete(el); }
      }));
    }
  }).observe(document.body,{childList:true,subtree:true,attributes:true,attributeFilter:['href','src']});
}
