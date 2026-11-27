function day(offset = 0) {
  const date = new Date();
  date.setDate(date.getDate() + offset);
  return new Intl.DateTimeFormat('sv-SE', { dateStyle: 'short' }).format(date);
}

const gameTheory = [
  { title: '收益矩阵', goal: '用两家商店的定价决策列出参与者、策略和收益。', reviewTask: '给两家商店各写两个策略，画出四种组合的收益。' },
  { title: '最佳回应', goal: '固定对手的策略，找出自己的最佳选择。', reviewTask: '改变一个收益数字，看看最佳回应会不会改变。' },
  { title: '纳什均衡', goal: '在同一张矩阵里找到双方都不愿单独改变的结果。', reviewTask: '从一道新题中指出均衡，并解释为什么双方都不愿单独改变。' },
];

function exampleCalendar() {
  return [
    { id: 'example-study', kind: 'study', date: day(), title: '向量的几何意义', detail: '用箭头解释方向和长度，再用坐标写出同一个向量。', course: '数学与物理基础', slug: 'math-map', completed: false },
    { id: 'example-review', kind: 'review', date: day(1), title: '线性变换', detail: '用把方格纸拉长的例子回忆线性变换的性质。', course: '数学与物理基础', slug: 'math-map', completed: false },
  ];
}

export async function learningAssistant({ path, method, request, json, state }) {
  state.studyCalendar ??= exampleCalendar();
  state.studyCourses ??= {};
  if (path === '/api/learning-calendar' && method === 'GET') {
    return json({ today: day(), items: state.studyCalendar });
  }
  if (path === '/api/learning-assistant/attachments' && method === 'POST') {
    const file = (await request.formData()).get('file');
    return file ? json({ name: file.name, text: '预览资料' }) : json({ error: '请选择文件。' }, 400);
  }
  if (path === '/api/learning-assistant/courses' && method === 'POST') {
    const { course, operationId } = await request.json();
    const slug = 'study-' + operationId;
    const existing = state.studyCourses[slug];
    if (existing) return json({ slug, title: existing.title, planned: existing.units.length });
    state.studyCourses[slug] = course;
    const now = new Date().toISOString();
    state.projects.unshift({ id: slug, slug, title: course.title, projectType: 'discipline-map', status: 'active',
      layoutVersion: 1, layoutStatus: 'current', overviewAvailable: true, createdAt: now, updatedAt: now });
    state.studyCalendar.push(...course.units.map((unit, index) => ({
      id: slug + '-unit-' + index, kind: 'study', date: unit.date, title: unit.title, detail: unit.goal,
      course: course.title, slug, topicId: 'unit-' + (index + 1), completed: false,
    })));
    return json({ slug, title: course.title, planned: course.units.length }, 201);
  }
  const unitRoute = /^\/api\/projects\/([^/]+)\/units\/([^/]+)\/open$/.exec(path);
  if (unitRoute && method === 'POST') {
    const [, courseSlug, topicId] = unitRoute;
    const slug = courseSlug + '-' + topicId;
    const course = state.studyCourses[courseSlug];
    if (!course) return json({ slug: 'math' });
    const unit = course.units[Number(topicId.slice(5)) - 1];
    if (!unit) return json({ error: '找不到这个学习单元。' }, 404);
    if (!state.projects.some(project => project.slug === slug)) {
      const now = new Date().toISOString();
      state.projects.unshift({ id: slug, slug, title: unit.title, projectType: 'system-learning', status: 'active',
        layoutVersion: 1, layoutStatus: 'current', createdAt: now, updatedAt: now });
    }
    return json({ slug });
  }
  const mapRoute = /^\/api\/projects\/([^/]+)\/(discipline-map|course-summaries)$/.exec(path);
  const course = mapRoute && state.studyCourses[mapRoute[1]];
  if (course && method === 'GET') {
    if (mapRoute[2] === 'course-summaries') return json({ lessons: [] });
    return json({ mapVersionId: 'confirmed', format: 'structured', overviewMarkdown: '',
      topics: { schemaVersion: 1, topics: course.units.map((unit, index) => ({
        id: 'unit-' + (index + 1), title: unit.title, goal: unit.goal, chapterTitle: '学习单元',
      })) },
    });
  }
  if (path === '/api/learning-assistant/settlements' && method === 'POST') return json({});
  if (path !== '/api/learning-assistant' || method !== 'POST') return;
  const { messages, reviewId, providerId } = await request.json();
  const reply = data => json({ ...data, ...(providerId?.startsWith('lwa-cloud:') && {
    creditUsage: { units: 1250, pending: false },
  }) });
  const turns = messages.filter(message => message.role === 'user');
  if (reviewId) {
    if (turns.at(-1)?.content.startsWith('现在来复习')) return reply({ reply: '假设你开了一家书店，附近另一家书店先降价。你会如何判断自己跟不跟？' });
    const task = state.studyCalendar.find(item => item.id === reviewId);
    if (task) task.completed = true;
    return reply({ reply: '你已经把这个判断用进了一个具体场景。', completedReview: reviewId });
  }
  if (turns.length === 1) return reply({ reply: '我们可以用两家商店定价串起收益矩阵、最佳回应和纳什均衡。你想用它分析什么？你现在的基础和可学习时间怎样？' });
  return reply({ reply: '我按三个具体问题排了一个方案。你看这个顺序和每天的安排合适吗？',
    course: { title: '博弈论', startDate: day(), weekdays: [0, 1, 2, 3, 4, 5, 6],
      units: gameTheory.map((unit, index) => ({ ...unit, date: day(index) })) },
  });
}

export function recordPreviewStudyCompletion(state, lessonSlug) {
  const event = state.studyCalendar?.find(item => item.kind === 'study' && !item.completed && lessonSlug === item.slug + '-' + item.topicId);
  if (!event) return;
  event.completed = true;
  const task = state.studyCourses[event.slug]?.units.find(unit => unit.title === event.title);
  for (const space of [1, 3, 7]) {
    state.studyCalendar.push({ ...event, id: event.id + '-review-' + space, kind: 'review', date: day(space), detail: task?.reviewTask, completed: false });
  }
}
