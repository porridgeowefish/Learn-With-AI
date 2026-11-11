import { projects, seedMethodology, seedProjects } from '/__design/projects.mjs';
import { initializeLearning, learning } from '/__design/learning.mjs';
import { settings } from '/__design/settings.mjs';
import { visuals } from '/__design/visuals.mjs';
import { cloud } from '/__design/cloud.mjs';




const clients = new Set();
const listeners = new Set();
export function subscribeEvents(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
const encode = new TextEncoder();
function emit(name, data) {
  for (const listener of listeners) listener(name, data);
  const bytes = encode.encode('event: ' + name + '\ndata: ' + JSON.stringify(data) + '\n\n');
  for (const client of clients) {
    try { client.enqueue(bytes); } catch { clients.delete(client); }
  }
}
function events() {
  let stream;
  const body = new ReadableStream({
    start(controller) {
      stream = controller;
      clients.add(stream);
      stream.enqueue(encode.encode('event: hello\ndata: {}\n\n'));
    },
    cancel() { clients.delete(stream); },
  });
  return new Response(body, {headers: {'Content-Type':'text/event-stream','Cache-Control':'no-store'}});
}

const database = new Promise((resolve, reject) => {
  const open = indexedDB.open('lll-design-preview', 1);
  open.onupgradeneeded = () => open.result.createObjectStore('workspace');
  open.onsuccess = () => resolve(open.result);
  open.onerror = () => reject(open.error);
});
const state = database.then(db => new Promise((resolve, reject) => {
  const read = db.transaction('workspace').objectStore('workspace').get('state');
  read.onsuccess = () => resolve(Object.assign(seedProjects(), read.result));
  read.onerror = () => reject(read.error);
})).then(async data => {
  const newStarter = !data.starterInitialized;
  if (newStarter) {
    if (!data.projects.some(project => project.slug === 'learning-methodology')) data.projects.unshift(seedMethodology());
    data.starterInitialized = true;
  }
  if (await initializeLearning(data) || newStarter) await save(data);
  return data;
});
async function save(data) {
  const db = await database;
  return new Promise((resolve, reject) => {
    const tx = db.transaction('workspace', 'readwrite');
    tx.objectStore('workspace').put(data, 'state');
    tx.oncomplete = resolve;
    tx.onabort = () => reject(tx.error);
  });
}
const json = (value, status = 200) => Response.json(value, {status});
export async function dispatch(request, url) {
  if (url.pathname === '/api/events') return events();
  const data = await state;
  let changed = false;
  const ctx = {request, url, path:url.pathname, method:request.method, state:data, json, emit, dirty() { changed = true; }};
  for (const handle of [projects, learning, settings, visuals, cloud]) {
    const response = await handle(ctx);
    if (response) {
      if (changed || (!['GET','HEAD'].includes(request.method) && response.ok)) await save(data);
      return response;
    }
  }
  console.error('Missing design handler', request.method, url.pathname);
  return json({error:'设计预览尚未登记此接口', path:url.pathname}, 501);
}
