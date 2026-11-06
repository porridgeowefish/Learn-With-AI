const stamp = () => new Date().toISOString();
const uuid = () => crypto.randomUUID();
function makeProject(slug, title, projectType, extra = {}) {
  return { id: slug, slug, title, projectType, status: 'active', layoutStatus: 'current', layoutVersion: 1,
    overviewAvailable: projectType === 'discipline-map', createdAt: stamp(), updatedAt: stamp(), ...extra };
}
function dateAt(offset) {
  const day = new Date();
  day.setDate(day.getDate() + offset);
  return [day.getFullYear(), String(day.getMonth()+1).padStart(2,'0'), String(day.getDate()).padStart(2,'0')].join('-');
}
export function seedProjects() {
  const projects = [
    makeProject('math-map', '数学与物理基础', 'discipline-map'),
    makeProject('math', '线性代数', 'system-learning'),
    makeProject('physics', '经典力学', 'system-learning'),
  ];
  const folders = [{ id: 'quantitative', name: '数学与物理基础', mapProjectSlug: 'math-map', slugOrder: ['math','physics'] }];
  const activity = [];
  const titles = ['梳理数学与物理的先修关系', '用几何图像解释特征向量', '用能量守恒检验运动方程'];
  for (let i = 0; i < 240; i++) {
    if (i > 3 && (i * 7 % 11 < 4 || i % 19 === 0)) continue;
    const p = projects[i % 3];
    activity.push({id:'event-'+i, sourceId:'study-'+i, sourceType:'reading', title:titles[i % 3],
      createdAt:dateAt(-i)+'T10:30:00', projectSlug:p.slug, projectTitle:p.title, activityDelta:1+i%8, delta:i%4});
  }
  return { projects, folders, activity, archivedProjects: [] };
}
function activitySummary(events, url) {
  const weeks = url.searchParams.get('weeks') === '52' ? 52 : 26;
  const filter = url.searchParams.get('project');
  const byDay = new Map();
  let totalGrowth = 0;
  for (const event of events) {
    if (filter && event.projectSlug !== filter) continue;
    totalGrowth += event.delta;
    const key = event.createdAt.slice(0,10);
    if (!byDay.has(key)) byDay.set(key, []);
    byDay.get(key).push(event);
  }
  let currentStreak = 0, longestStreak = 0, activeDays = 0, totalActions = 0;
  const dayCount = (weeks - 1) * 7 + ((new Date().getDay() + 6) % 7) + 1;
  const days = Array.from({length: dayCount}, (_,i) => {
    const date = dateAt(i-dayCount+1);
    const entries = byDay.get(date) || [];
    const activity = entries.reduce((sum,e) => sum+e.activityDelta, 0);
    const growth = entries.reduce((sum,e) => sum+e.delta, 0);
    if (activity > 0) currentStreak++;
    else if (i < dayCount - 1) currentStreak = 0;
    longestStreak = Math.max(currentStreak,longestStreak);
    if (entries.length) activeDays++;

    totalActions += entries.length;
    return {date, events: entries, actions: entries.length, activity, growth};
  });
  return {rangeStart: days[0].date, rangeEnd: days.at(-1).date, days, currentStreak, longestStreak, activeDays, totalActions, totalGrowth};
}
export async function projects({url, path, method, request, state, json, emit}) {
  if (path === '/api/activity' && method === 'GET') return json({summary:activitySummary(state.activity,url)});
  if (path === '/api/folders') {
    if (method === 'PUT') state.folders = (await request.json()).folders;
    return json({folders:state.folders});
  }
  if (path === '/api/project-type-advice' && method === 'POST') {
    const input = await request.json();
    const broad = /地图|领域|全貌|体系|方向|学科/.test([input.title,input.why,...input.messages.filter(m=>m.role==='user').map(m=>m.content)].join(' '));
    return json({reply: broad ? '建议先建学科地图，梳理主题与先修关系，再选择一个主题开始学习。' : '建议建立对话项目。从一个具体问题开始，用练习检验理解。',
      recommendation:broad?'discipline-map':'system-learning', confidence:'medium'});
  }
  if (path === '/api/projects') {
    if (method === 'GET') return json({projects:state.projects});
    if (method !== 'POST') return;
    const input = await request.json();
    const title = input.title?.trim();
    if (!title) return json({error:'请输入项目名称'},400);
    const slug = input.slug?.trim() || title.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-|-$/g, '');
    if (!/^[a-z0-9\p{Ll}\p{Lo}][a-z0-9\p{Ll}\p{Lo}-]*$/u.test(slug) || new TextEncoder().encode(slug).length > 128) {
      return json({error:'项目标识无效'},400);
    }
    const type = input.projectType || 'system-learning';
    if (!['discipline-map','system-learning'].includes(type)) return json({error:'项目类型无效'},400);
    if (input.learningMode && !['general','history','book','concept'].includes(input.learningMode)) return json({error:'学习方式无效'},400);
    if (input.scopeSource && (type !== 'system-learning' || !input.scopeSource.topicId ||
      !state.projects.some(p => p.projectType === 'discipline-map' && p.slug === input.scopeSource.mapSlug))) {
      return json({error:'学习范围来源无效'},400);
    }
    if (state.projects.some(p=>p.slug === slug)) return json({error:'项目已存在'},409);
    const project = makeProject(slug,title,type, { scopeSource:input.scopeSource, systemPrompt:input.systemPrompt, ...(type === 'system-learning' ? {learningMode: input.learningMode || 'general'} : {}) });
    state.projects.push(project);
    if (type === 'discipline-map') {
      const existing = state.folders.find(f => !f.mapProjectSlug && f.name === title);
      if (existing) existing.mapProjectSlug = slug;
      else state.folders.push({id:uuid(),name:title,mapProjectSlug:slug,slugOrder:[]});
    }
    if (input.scopeSource) state.folders.find(f=>f.mapProjectSlug === input.scopeSource.mapSlug)?.slugOrder.push(slug);
    return json({project},201);
  }
  const match = /^\/api\/projects\/([^/]+)(?:\/(activity|archive))?$/.exec(path);
  if (!match) return;
  const slug = decodeURIComponent(match[1]);
  const project = state.projects.find(p=>p.slug === slug);
  if (!project) return json({error:'找不到项目'},404);
  if (match[2] === 'activity' && method === 'POST') {
    const event = await request.json();
    if (!state.activity.some(e=>e.id===event.id && e.projectSlug===slug)) state.activity.push({delta:0,activityDelta:1,sourceType:'reading',...event,createdAt:stamp(),projectSlug:slug,projectTitle:project.title});
    emit('learning-activity-updated',{projectSlug:slug});
    return json({ok:true});
  }
  if ((!match[2] && method === 'DELETE') || (method === 'POST' && match[2] === 'archive')) {
    if (match[2] === 'archive') state.archivedProjects.push({...project,status:'archived'});
    if (state.learning) delete state.learning[slug];
    state.projects = state.projects.filter(p=>p.slug !== slug);
    state.activity = state.activity.filter(e=>e.projectSlug !== slug);
    for (const folder of state.folders) {
      folder.slugOrder = folder.slugOrder.filter(s=>s!==slug);
      if (folder.mapProjectSlug === slug) delete folder.mapProjectSlug;
    }
    return json({deleted:true,projectId:project.id});
  }
  if (!match[2] && ['PATCH','PUT'].includes(method)) {
    const {title} = await request.json();
    if (!title?.trim()) return json({error:'请输入项目名称'},400);
    project.title = title.trim();
    project.updatedAt = stamp();
  }
  if (!match[2] && ['GET','PATCH','PUT'].includes(method)) return json({project});
  return json({error:'请求方法不受支持'},405);
}
