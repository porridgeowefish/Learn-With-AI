const now = () => new Date().toISOString();
const id = () => crypto.randomUUID();
const reply =
  '向量加法表示连续发生的位移。把第二个向量的起点移到第一个向量的终点，再连接最初的起点和最后的终点，就得到合位移。';
const review =
  '# 向量与坐标\n\n## 核心概念\n\n向量同时具有大小和方向。用一对坐标表示它，可以分别计算两个方向上的变化。\n\n## 一个例子\n\n向东走三步，再向北走四步，位移的大小为五步。';
const seedSlugs = new Set(['math', 'physics', 'math-map']);
const physicsReply =
  '先选定小球和地球作为系统。忽略空气阻力时，小球下落减少的重力势能转化成动能，两者之和保持不变。';
const physicsReview =
  '# 机械能守恒\n\n## 系统与条件\n\n只有重力或弹簧弹力做功时，系统的动能与势能之和保持不变。\n\n## 一个例子\n\n忽略空气阻力，自由落体的重力势能转化成动能。';
// Simulate the teacher's per-turn footer using the same wire format as production.
function previewAnswer(physics, question = '') {
  if (!question) return physics
    ? '小球下落时高度降低、速度增大。忽略空气阻力，减少的重力势能主要转化成哪种能量？\n\n```suggested-replies\n["动能","内能","化学能","还不确定，先看讲解"]\n```'
    : '向量加法表示连续发生的位移。先向东走 3 步，再向西走 2 步，最后停在哪里？\n\n```suggested-replies\n["向东 1 步","向东 5 步","回到起点","还不确定，先看讲解"]\n```';
  if (question === '还不确定，先看讲解' || question === '跳过') return physics
    ? '可以，先看讲解。小球落下时速度变快，说明动能增加；忽略空气阻力，增加的动能等于减少的重力势能。'
    : '可以，先看讲解。向东记为正、向西记为负，3 + (−2) = 1，所以最后在起点东边 1 步处。方向相反的位移会抵消一部分。';
  if (physics && ['动能', '内能', '化学能'].includes(question)) return question === '动能'
    ? '这次判断正确：速度增大意味着动能增加。忽略空气阻力时，减少的重力势能转化成动能。'
    : '这里应选择动能。小球速度增大，动能随之增加；题目忽略了空气阻力，所以不考虑阻力造成的机械能向内能转化。';
  if (!physics && ['向东 1 步', '向东 5 步', '回到起点'].includes(question)) return question === '向东 1 步'
    ? '这次判断正确。两段位移方向相反，3 步向东与 2 步向西抵消后，还剩 1 步向东。向量相加时要同时考虑大小和方向。'
    : '最后在起点东边 1 步处。两段路方向相反，不能把长度直接相加；向东 3 步再退回 2 步，仍留有 1 步向东的位移。';
  if (!physics && question === '反向走呢') {
    return '先向东走 3 步，再向西走 2 步，最后停在起点东边 1 步处。看图时沿两条路线依次走，比较 A 和 C 的位置。\n\n```svg\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 110"><g fill="none" stroke-width="3"><path stroke="#34756c" d="M30 32H255m-10 -6 10 6-10 6"/><path stroke="#b15c38" d="M255 60H105m10 -6-10 6 10 6"/></g><g font-size="14"><text x="15" y="24">A 起点</text><text x="218" y="24">B 折返</text><text x="82" y="86">C 终点</text></g></svg>\n```';
  }
  if (question === '算算长度') return '如果两段互相垂直的位移分别是 3 步和 4 步，合位移就是直角三角形的斜边，长度为 5 步。';
  if (question === '为何守恒') return '忽略空气阻力时，小球减少的重力势能全部转化成动能，两者之和保持不变。';
  if (question === '结合交互图表理解') {
    return physics
      ? '拖动滑块改变小球高度。假设总机械能为 100 J，观察两项能量之和。\n\n```html-interactive\n<label>高度占比 <input id="h" type="range" min="0" max="100" value="50"></label><output id="energy">势能 50 J，动能 50 J</output><script>const h=document.querySelector("#h");h.oninput=()=>document.querySelector("#energy").textContent="势能 "+h.value+" J，动能 "+(100-h.value)+" J";</script>\n```'
      : '拖动滑块改变第二段位移。虚线连接起点和终点，表示合位移。\n\n```html-interactive\n<label>第二段位移 <input id="step" type="range" min="-4" max="4" value="2"></label><svg viewBox="0 0 200 140"><path d="M20 70H120" fill="none" stroke="#34756c" stroke-width="3"/><line id="b" x1="120" y1="70" x2="120" y2="46" stroke="#b15c38" stroke-width="3"/><line id="sum" x1="20" y1="70" x2="120" y2="46" stroke="#586a96" stroke-width="2" stroke-dasharray="5 3"/></svg><script>document.querySelector("#step").oninput=e=>{for(const id of ["#b","#sum"])document.querySelector(id).setAttribute("y2",70-12*e.target.value)}</script>\n```';
  }
  if (question === '举个例子') return physics ? '松开手中的球，它越落越快：高度降低时，重力势能逐渐转化成动能。' : '向东走三步，再向北走四步。虽然走了七步，你离出发点只有五步。';
  if (question === '太难，退一步') return physics ? '先看一个熟悉的现象：松开手中的球，它会越落越快。速度变快意味着动能增加。' : '先把位移看作一根箭头：箭头朝哪儿，你就向哪儿移动。';
  if (question === '太简单，快一点') return physics ? '加入空气阻力后，部分机械能转化为内能，机械能不再守恒，但包含内能在内的总能量仍然守恒。' : '两段位移长度不变时，夹角越大，合位移越短；夹角达到 180 度时，长度等于两段位移长度之差的绝对值。';
  return physics ? physicsReply : reply;
}
function practice(count = 2, physics = false) {
  return (
    '# 练习\n\n' +
    Array.from({ length: count }, (_, i) => {
      const x = i + 1;
      if (physics)
        return `## 第 ${x} 题\n题型：single-choice · 难度：1/5\n\n忽略阻力，小球下落时重力势能减少 ${x} J，动能增加多少？\n\nA. ${x} J\nB. ${x + 1} J\nC. 0 J\n\n### 参考答案\nA\n\n### 解析\n系统机械能守恒，减少的势能全部转化为动能。`;
      return `## 第 ${x} 题\n题型：single-choice · 难度：1/5\n\n向量 $(${x}, 2)$ 与 $(1, 0)$ 相加后的横坐标是多少？\n\nA. ${x}\nB. ${x + 1}\nC. ${x + 2}\n\n### 参考答案\nB\n\n### 解析\n横坐标分别相加得到 ${x + 1}。`;
    }).join('\n\n')
  );
}
function message(role, source, extra = {}) {
  return {
    id: id(),
    role,
    blocks: [{ id: id(), type: 'markdown', source }],
    status: 'completed',
    createdAt: now(),
    ...extra,
  };
}
function asset(key, content) {
  return {
    meta: {
      assetId: key,
      key,
      title: key === 'review' ? '复习稿' : '练习',
      currentVersionId: id(),
      editRevision: 1,
      conversationCursor: 1,
      updatedAt: now(),
    },
    content,
    contentKind: 'markdown',
  };
}
function source(name, file, parsedText = '') {
  const sourceId = id(),
    revisionId = id(),
    createdAt = now();
  return {
    source: {
      sourceId,
      displayName: name,
      currentRevisionId: revisionId,
      derivedVersionId: revisionId,
      status: parsedText ? 'ready' : 'opaque',
      extractedBytes: new Blob([parsedText]).size,
      coverage: { complete: Boolean(parsedText) },
      createdAt,
      updatedAt: createdAt,
    },
    revision: {
      sourceId,
      revisionId,
      createdAt,
      original: {
        key: 'original',
        path: name,
        filename: name,
        mediaType: file.type,
        bytes: file.size,
        sha256: '',
      },
      derivedFiles: parsedText
        ? [
            {
              key: 'content',
              path: 'derived/content.md',
              mediaType: 'text/markdown',
              bytes: new Blob([parsedText]).size,
              sha256: '',
            },
          ]
        : [],
    },
    parsedText,
    file,
    jobs: [],
  };
}
function initialize(project) {
  const seeded = seedSlugs.has(project.slug);
  const physics = project.slug === 'physics';
  const lesson = physics ? physicsReview : review;
  const mapTopics =
    project.slug === 'math-map'
      ? ['向量与线性变换', '牛顿运动定律', '机械能守恒']
      : ['向量的表示', '向量加法', '坐标分解'];
  const assets = {
    review: asset('review', seeded ? lesson : ''),
    practice: asset('practice', seeded ? practice(2, physics) : ''),
  };
  const topics = mapTopics.map((title, i) => ({
    id: 'topic-' + i,
    title,
    chapterTitle: project.title,
    priority: 'I',
    goal: '用一个具体例子解释' + title,
    inScope: [title],
    outOfScope: [],
    prerequisites: [],
    ownedConcepts: [title],
    reusedConcepts: [],
  }));
  return {
    physics,
    assets,
    topics,
    plan: { schemaVersion: 1, items: [], updatedAt: now() },
    mapVersion: seeded ? id() : '',
    messages: seeded
      ? [
          message(
            'learner',
            physics ? '自由落体时能量如何变化？' : '怎样理解向量加法？',
          ),
          message('teacher', previewAnswer(physics)),
        ]
      : [],
    sources: seeded
      ? [
          source(
            physics ? '自由落体实验手记.md' : '向量学习手记.md',
            new Blob([lesson], { type: 'text/markdown' }),
            lesson,
          ),
        ]
      : [],
    queue: [],
    media: {},
    jobs: [],
    annotations: [],
    sessions: [],
    rounds: seeded
      ? [
          {
            versionId: assets.practice.meta.currentVersionId,
            createdAt: now(),
            content: assets.practice.content,
          },
        ]
      : [],
    results: {},
    notes: {
      entries: seeded
        ? [
            {
              id: id(),
              jobId: 'note',
              content: physics
                ? '忽略阻力时，自由落体的动能与重力势能之和保持不变。'
                : '向量相加时，分别相加横坐标和纵坐标。',
              fromTurn: 1,
              throughTurn: 1,
              createdAt: now(),
              providerId: 'preview',
              model: 'demo',
            },
          ]
        : [],
      jobs: [],
    },
    systemPrompt: project.systemPrompt || '',
  };
}
function stream(frames, delay = 0, interrupted = () => false) {
  let index = 0;
  return new Response(
    new ReadableStream({
      async pull(controller) {
        if (delay && index > 1)
          await new Promise((done) => setTimeout(done, delay));
        let [event, data] = frames[index++];
        if (interrupted()) {
          event = 'message-failed';
          data = { ...data, code: 'stopped', partialPreserved: true };
          index = frames.length;
        }
        const wire =
          (event ? 'event: ' + event + '\n' : '') +
          'data: ' +
          JSON.stringify(data) +
          '\n\n';
        controller.enqueue(new TextEncoder().encode(wire));
        if (index === frames.length) controller.close();
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream' } },
  );
}
const answerStream = (text) =>
  stream([
    ['', { type: 'text', content: text }],
    ['', { type: 'done' }],
  ]);
function releaseQueue(s) {
  for (const item of s.queue.splice(0)) {
    s.messages.push(
      message('learner', item.content, { operationId: item.operationId }),
      message('teacher', previewAnswer(s.physics, item.content)),
    );
  }
}
function conversation(ctx, s, parts, b) {
  const { method, json } = ctx;
  const [action, key, op] = parts;
  if (
    !action &&
    s.queue.length &&
    !s.queuePaused &&
    Date.now() >= (s.streamingUntil || 0)
  ) {
    releaseQueue(s);
    ctx.dirty();
  }
  if (!action)
    return json({
      conversationId: 'preview',
      unitId: 'preview',
      latestSeq: s.messages.length,
      pageFromSeq: 1,
      pageThroughSeq: s.messages.length,
      totalMessages: s.messages.length,
      hasMore: false,
      hasPrevious: false,
      messages: s.messages,
      queue: s.queue,
      queuePaused: !!s.queuePaused,
      latestResponseId: s.responseId,
    });
  if (action === 'instructions') {
    if (method === 'PUT') s.systemPrompt = b.systemPrompt;
    return json({ systemPrompt: s.systemPrompt });
  }
  if (action === 'export.md')
    return new Response(
      s.messages.flatMap((m) => m.blocks.map((x) => x.source)).join('\n\n'),
      { headers: { 'Content-Type': 'text/markdown; charset=utf-8' } },
    );
  if (action === 'attachments') {
    if (method === 'POST') {
      const attachmentId = id();
      s.media[attachmentId] = b.file;
      return json({
        attachmentId,
        kind: b.file.type.startsWith('audio/') ? 'audio' : 'image',
        name: b.file.name,
        status: 'ready',
        text: '',
      });
    }
    if (s.media[key]) return new Response(s.media[key]);
  }
  if (key === 'active' || (action === 'opening' && s.messages.length))
    return new Response(null, { status: 204 });
  if (op === 'stop') {
    if (key === s.responseId) {
      s.queuePaused = true;
      s.stoppedId = key;
      const m = s.messages.at(-1);
      if (m?.role === 'teacher') m.status = 'interrupted';
    }
    return json({ stopped: true });
  }
  if (action === 'turns' && key === 'queue') {
    const item = { ...b, queueId: id(), queuedAt: now() };
    s.queue.push(item);
    return json({ queueId: item.queueId, promoted: false });
  }
  if (action === 'queue') {
    if (key === 'resume') {
      s.queuePaused = false;
      releaseQueue(s);
      return json({ resumed: true });
    }
    const item = s.queue.find((q) => q.queueId === key);
    if (!item) return json({ error: '队列项不存在' }, 404);
    if (op === 'edit') {
      Object.assign(item, b);
      return json(item);
    }
    s.queue = s.queue.filter((q) => q !== item);
    if (op === 'discard') return json({ queueId: key, discarded: true });
    b = item;
  }
  if (['turns', 'opening', 'responses', 'queue'].includes(action)) {
    const learner = message('learner', b.content || '', {
      operationId: b.operationId,
    });
    learner.blocks.push(...(b.mediaBlocks || []));
    for (const sourceId of b.attachmentRefs || []) {
      const row = s.sources.find((x) => x.source.sourceId === sourceId);
      if (row)
        learner.blocks.push({
          id: id(),
          type: 'attachment',
          artifactRef: sourceId,
          name: row.source.displayName,
        });
    }
    for (const attachmentId of b.conversationAttachmentIds || []) {
      const file = s.media[attachmentId];
      if (file)
        learner.blocks.push({
          id: id(),
          type: file.type.startsWith('audio/') ? 'chat-audio' : 'chat-image',
          artifactRef: attachmentId,
          name: file.name,
        });
    }
    if (b.content || learner.blocks.length > 1) s.messages.push(learner);
    if (op === 'regenerate' && s.messages.at(-1)?.role === 'teacher')
      s.messages.pop();
    let answer = previewAnswer(s.physics, b.content || s.messages.findLast(m => m.role === 'learner')?.blocks[0]?.source);
    const controls = ctx.state.settings?.learning;
    if (controls?.length === 'short' && !answer.includes('```')) answer = answer.split('。')[0] + '。';
    if (controls?.length === 'long' && !answer.includes('```')) answer += '\n\n我们可以再用坐标验证：先向东走 3 步，再向北走 4 步，得到位移 (3, 4)。横纵坐标分别记录两个方向的变化。';
    const searched = controls?.search.enabled && b.options?.webSearch;
    if (searched) {
      const searchText = '\n\n搜索结果演示：[向量资料](https://zh.wikipedia.org/wiki/向量)。';
      const footer = answer.indexOf('\n\n```suggested-replies\n');
      answer = footer < 0 ? answer + searchText : answer.slice(0, footer) + searchText + answer.slice(footer);
    }
    const teacher = message('teacher', answer);
    const reasoning = controls?.showReasoning && b.options?.effort ? '设计预览：先确认概念与条件，再通过例子核对结论。实际摘要由所选模型提供。' : '';
    if (reasoning) teacher.blocks.push({id:id(),type:'reasoning-summary',source:reasoning});
    if (controls?.memoryEnabled && b.content && /我.*(学过|学习|希望|喜欢|目标)/.test(b.content) && !ctx.state.forgottenMemories?.includes(b.content)) {
      ctx.state.memories ||= [];
      if(!ctx.state.memories.some(m=>m.evidence===b.content))ctx.state.memories.push({id:id(),text:b.content,project:'',sourceProject:ctx.path.split('/')[3],messageId:learner.id,evidence:b.content,updatedAt:now(),edited:false});
    }
    s.messages.push(teacher);
    const deltas = answer.match(/[\s\S]{1,40}/gu) || [];
    s.streamingUntil = Date.now() + 300 * (deltas.length + 1);
    const responseId = (s.responseId = id());
    return stream(
      [
        [
          'turn-accepted',
          { learnerMessageId: learner.id, responseId: s.responseId },
        ],
        ['message-started', { teacherMessageId: teacher.id }],
        ...(reasoning ? [['reasoning-summary-delta',{blockId:teacher.blocks[1].id,delta:reasoning}]] : []),
        ...(searched ? [['search-started',{query:b.content}],['search-completed',{query:b.content,count:1}]] : []),
        ...deltas.map((delta) => [
          'text-delta',
          { blockId: teacher.blocks[0].id, delta },
        ]),
        [
          'message-completed',
          { messageId: teacher.id, latestSeq: s.messages.length },
        ],
      ].map(([event, data], i) => [
        event,
        { ...data, responseId: s.responseId, frameSeq: i + 1 },
      ]),
      300,
      () => s.stoppedId === responseId,
    );
  }
}
async function sources(ctx, s, parts, b) {
  const { method, json } = ctx;
  const [key, action] = parts;
  if (!key) {
    if (method === 'GET')
      return json({ sources: s.sources.map((x) => x.source) });
    const text =
      /\.(md|txt|csv|markdown)$/i.test(b.file.name) ||
      b.file.type.startsWith('text/')
        ? await b.file.text()
        : '';
    const row = source(b.displayName || b.file.name, b.file, text);
    s.sources.push(row);
    return json({
      source: row.source,
      revision: row.revision,
      parseDisposition: row.source.status,
      parseReason: '',
    });
  }
  const row = s.sources.find((x) => x.source.sourceId === key);
  if (!row) return json({ error: '资料不存在' }, 404);
  if (action === 'structure-jobs') {
    if (method === 'GET')
      return json({ jobs: row.jobs, localOCRAvailable: true });
    const job = {
      ...b,
      id: id(),
      revisionId: row.revision.revisionId,
      status: 'succeeded',
    };
    row.jobs.unshift(job);
    return json({ job });
  }
  if (action === 'revisions' && parts[2] === row.revision.revisionId) {
    if (parts[4] === 'content' && row.parsedText)
      return new Response(row.parsedText, {
        headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
      });
    if (parts[4] === 'original' && row.file) return new Response(row.file);
    return json({ error: '文件不可用' }, 404);
  }
  if (action === 'evidence')
    return json({
      quote: row.parsedText || row.source.displayName,
      displayRange: '第 1 行',
    });
  if (method === 'DELETE' || action === 'permanent-delete') {
    s.sources = s.sources.filter((x) => x !== row);
    return json({ deleted: true });
  }
  if (action === 'retry') return json(row.source);
  return json({ source: row.source, revision: row.revision });
}
function assets(ctx, s, parts, b) {
  const { json, method } = ctx;
  const [key, action, entry, op] = parts;
  if (!key) return json({ assets: Object.values(s.assets).map((a) => a.meta) });
  if (action === 'rounds') return json({ rounds: s.rounds });
  if (action === 'results') {
    if (method === 'GET') return json({ results: s.results });
    if (method === 'DELETE') {
      delete s.results[entry];
      return json({});
    }
    return json((s.results[entry] = { ...b, submittedAt: now() }));
  }
  if (action === 'annotations') {
    if (method === 'GET') return json({ annotations: s.annotations });
    if (method === 'DELETE') {
      s.annotations = s.annotations.filter((a) => a.annotationId !== entry);
      return json({});
    }
    if (!entry) {
      const annotation = {
        ...b,
        annotationId: id(),
        assetId: key,
        schemaVersion: 1,
        status: 'open',
        createdAt: now(),
        updatedAt: now(),
      };
      s.annotations.push(annotation);
      return json({ annotation });
    }
    const annotation = s.annotations.find((a) => a.annotationId === entry);
    if (!annotation) return json({ error: '批注不存在' }, 404);
    annotation.ask ??= { messages: [] };
    if (op === 'ask-stream') {
      for (const [role, content] of [
        ['learner', b.content],
        ['assistant', reply],
      ])
        annotation.ask.messages.push({
          id: id(),
          role,
          content,
          status: 'completed',
          createdAt: now(),
        });
      annotation.status = 'asked';
      return answerStream(reply);
    }
    annotation.ask.summary = reply;
    annotation.ask.summaryState = 'done';
    return json({});
  }
  const item = s.assets[key];
  if (!item) return;
  if (method === 'PUT') {
    if (item.meta.editRevision !== b.baseEditRevision)
      return json(
        {
          error: {
            code: 'asset_edit_conflict',
            message: '资产已被其他编辑更新',
            currentEditRevision: item.meta.editRevision,
          },
        },
        409,
      );
    item.content = b.content;
    item.meta.editRevision++;
    item.meta.updatedAt = now();
  }
  return json(item);
}
function finishGeneration(s, job) {
  job.status = 'succeeded';
  job.phase = 'done';
  job.progress = { completedUnits: 1, totalUnits: 1, unitLabel: '份' };
  if (job.kind === 'discipline-map') s.mapVersion = job.id;
  else if (s.assets[job.kind]) {
    const item = asset(
      job.kind,
      job.kind === 'review'
        ? s.physics
          ? physicsReview
          : review
        : practice(job.questionCount, s.physics),
    );
    item.meta.currentVersionId = job.id;
    s.assets[job.kind] = item;
    if (job.kind === 'practice')
      s.rounds.unshift({
        versionId: item.meta.currentVersionId,
        createdAt: now(),
        content: item.content,
      });
  }
}

function addLearningState(state, project) {
  state.learning ??= Object.create(null);
  if (Object.hasOwn(state.learning, project.slug)) return false;
  Object.defineProperty(state.learning, project.slug, {
    value: initialize(project),
    enumerable: true,
    configurable: true,
    writable: true,
  });
  return true;
}
export function initializeLearning(state) {
  let changed = false;
  for (const project of state.projects) {
    if (!seedSlugs.has(project.slug)) continue;
    if (addLearningState(state, project)) changed = true;

  }
  return changed;
}
export async function learning(ctx) {
  const { path, state, json, method, request, emit } = ctx;
  const [, api, projects, rawSlug, feature, ...parts] = path.split('/');
  if (
    api !== 'api' ||
    projects !== 'projects' ||
    ![
      'conversation',
      'sources',
      'assets',
      'notes',
      'discipline-map',
      'learning-plan',
      'generation-jobs',
      'feynman',
    ].includes(feature)
  )
    return;
  const slug = decodeURIComponent(rawSlug);
  const project = state.projects.find((p) => p.slug === slug);
  if (!project) return json({ error: '项目不存在' }, 404);
  if (addLearningState(state, project)) ctx.dirty();
  const s = state.learning[slug];
  for (const job of s.jobs) {
    if (
      job.status === 'running' &&
      Date.now() - Date.parse(job.updatedAt) >= 3000
    ) {
      finishGeneration(s, job);
      ctx.dirty();
      emit('generation-job-updated', {
        ...job,
        projectSlug: slug,
        jobId: job.id,
      });
    }
  }
  const type = request.headers.get('content-type') || '';
  const b = type.includes('multipart/form-data')
    ? Object.fromEntries(await request.formData())
    : type.includes('application/json')
      ? await request.json()
      : {};
  if (feature === 'conversation') return conversation(ctx, s, parts, b);
  if (feature === 'sources') return sources(ctx, s, parts, b);
  if (feature === 'assets') return assets(ctx, s, parts, b);
  if (feature === 'learning-plan') {
    if (method === 'PUT') s.plan = b;
    return json(s.plan);
  }
  if (feature === 'discipline-map')
    return json({
      mapVersionId: s.mapVersion,
      format: 'structured',
      overviewMarkdown: s.mapVersion
        ? `## 研究目的\n梳理${project.title}的主要概念与先修关系。\n\n## ${project.title}\n### 核心主题\n` +
          s.topics.map((t) => '#### ' + t.title + '\n' + t.goal).join('\n')
        : '尚未生成学科总览',
      topics: {
        schemaVersion: 1,
        topics: s.mapVersion ? s.topics : [],
        updatedAt: now(),
      },
    });
  if (feature === 'notes')
    return json(
      parts.length
        ? (s.notes.jobs.find((x) => x.id === parts[1]) ?? {})
        : s.notes,
    );
  if (feature === 'feynman') {
    if (method === 'GET') return json({ sessions: s.sessions });
    if (parts.length === 1) {
      const session = {
        sessionId: id(),
        messages: [],
        reviewVersionId: s.assets.review.meta.currentVersionId,
        createdAt: now(),
        updatedAt: now(),
      };
      s.sessions.unshift(session);
      return json(session);
    }
    const session = s.sessions.find((x) => x.sessionId === parts[1]);
    if (!session) return json({ error: '讲解不存在' }, 404);
    const text = b.content
      ? '如果横坐标为负数，图上会有什么变化？'
      : '请把我当作刚接触向量的同学，从一个例子讲起吧。';
    for (const [role, content] of [
      ['learner', b.content],
      ['teacher', text],
    ])
      if (content)
        session.messages.push({
          id: id(),
          role,
          content,
          status: 'completed',
          createdAt: now(),
        });
    session.updatedAt = now();
    return answerStream(text);
  }
  const previous = s.jobs.find((j) => j.id === parts[0]);
  if (method === 'GET')
    return parts[1] === 'candidate'
      ? json({ content: s.assets[previous?.kind]?.content || '' })
      : json({ jobs: s.jobs });
  if (parts[1] === 'cancel' && previous) {
    previous.status = 'cancelled';
    return json({ job: previous });
  }
  const job = {
    ...previous,
    ...b,
    id: id(),
    rootJobId: previous?.rootJobId || id(),
    attempt: (previous?.attempt || 0) + 1,
    status: 'running',
    phase: 'generating',
    progress: { indeterminate: true },
    providerId: 'preview',
    model: 'demo',
    updatedAt: now(),
  };
  s.jobs.unshift(job);
  emit('generation-job-updated', { ...job, projectSlug: slug, jobId: job.id });
  return json({ job });
}
