import { useEffect, useRef, useState } from 'react';
import { Button } from '@/shared/primitive/Button';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { Modal } from '@/shared/primitive/Modal';
import { PracticeReader } from './PracticeReader';
import { ReviewReader } from './ReviewReader';
import { demoNotes, demoPractice, demoQuestions, demoReview, demoReviewPages, demoSentence, demoSource } from './learningDemoContent';
import s from './LearningDemo.module.css';

const steps = [
  { title: '教师对话', instruction: '选一个预置问题，看看老师如何把直觉、计算和边界连起来。真实学习时，在教师页输入自己的问题即可。' },
  { title: '复习与批注', instruction: '用页码切换正文与总复习。选中一句话保存批注，或点“选择示例句”试一次，再看看预置答疑。真实复习页也可划词问 AI。' },
  { title: '练习', instruction: '先尝试作答，再展开“参考答案与解析”。真实项目可生成新一轮练习，并翻看以前的轮次。' },
  { title: '笔记', instruction: '在这里写下自己的理解。真实项目的笔记与复习页也有笔记，自动笔记和自己的复习批注各自保留。' },
  { title: '附件', instruction: '展开资料看看依据。真实学习时，在对话栏左侧上传附件，或进入“附件”管理文件；教师可据此阅读并提供原文引用。' },
  { title: '学科地图', instruction: '展开节点，看看知识如何组织。真实学习时，新建“学科地图”梳理领域，再为其中的主题创建系统学习项目。' },
] as const;

export function LearningDemo({ onClose, onStart, onConfigure }: { onClose: () => void; onStart: () => void; onConfigure?: () => void }) {
  const [step, setStep] = useState(0);
  const [question, setQuestion] = useState(0);
  const [annotations, setAnnotations] = useState<string[]>([]);
  const [notes, setNotes] = useState(demoNotes);
  const layoutRef = useRef<HTMLDivElement>(null);
  const move = (next: number) => setStep(next);
  useEffect(() => { layoutRef.current?.parentElement?.scrollTo?.({ top: 0 }); }, [step]);

  return <Modal open onOpenChange={open => { if (!open) onClose(); }} title="跟着示例学一次" description="梯度下降 · 预置内容，无需 API。本次操作仅供体验，关闭后重置。" size="lg" className={s.modal}
    footer={<div className={s.footer}>
      <Button variant="ghost" onClick={onClose}>跳过引导</Button>
      <span className={s.counter}>第 {step + 1} / {steps.length} 步</span>
      <div className={s.actions}>
        <Button onClick={() => move(step - 1)} disabled={step === 0}>上一步</Button>
        {step < steps.length - 1 ? <Button variant="primary" onClick={() => move(step + 1)}>下一步</Button> : <Button variant="primary" onClick={onStart}>开始自己的学习</Button>}
      </div>
    </div>}>
    <div className={s.layout} ref={layoutRef}>
      <nav className={s.steps} aria-label="示例体验步骤">
        <span className={s.eyebrow}>从一个问题开始</span>
        {steps.map((item, index) => <button type="button" key={item.title} aria-current={step === index ? 'step' : undefined} className={step === index ? s.active : ''} onClick={() => move(index)}><span>{index + 1}</span>{item.title}</button>)}
        <p>随时跳到感兴趣的一步，或按顺序走一遍。</p>
      </nav>
      <div className={s.stage}>
        <header className={s.guide} aria-live="polite"><span className={s.eyebrow}>体验 · {steps[step].title}</span><p>{steps[step].instruction}</p></header>
        {step === 0 && <section className={s.teacher} aria-label="预置教师问答">
          <h2>一个算法，三个好问题</h2>
          <div className={s.questions}>{demoQuestions.map((item, index) => <button type="button" key={item.question} aria-pressed={question === index} onClick={() => setQuestion(index)}>{item.question}</button>)}</div>
          <article className={s.exchange} aria-live="polite"><span className={s.speaker}>学习者</span><p>{demoQuestions[question].question}</p><span className={s.speaker}>教师 · 预置回答</span><MarkdownView source={demoQuestions[question].answer} /></article>
        </section>}
        {step === 1 && <div className={s.review}>
          <ReviewReader slug="" content={demoReview} assetVersionId="guided-example-v1" renderBody={pageIndex => <DemoReviewBody key={pageIndex} pageIndex={pageIndex} onSave={quote => setAnnotations(current => current.includes(quote) ? current : [...current, quote])} />} />
          {annotations.length > 0 && <aside className={s.saved}><h3>本次批注 · {annotations.length}</h3>{annotations.map(quote => <blockquote key={quote}>{quote}</blockquote>)}</aside>}
        </div>}
        <div hidden={step !== 2} className={s.padded}><p className={s.note}>临时作答：提交后自动判分，示例成绩不会保存。</p><PracticeReader content={demoPractice} versionId="guided-example-v1" /></div>
        {step === 3 && <section className={s.padded}><label className={s.noteField}>我的示例笔记<textarea value={notes} onChange={event => setNotes(event.target.value)} /></label><p className={s.note} role="status">修改会在本次体验中保留。</p>{annotations.length > 0 && <aside className={s.saved}><h3>刚才保存的复习批注</h3>{annotations.map(quote => <blockquote key={quote}>{quote}</blockquote>)}</aside>}</section>}
        {step === 4 && <section className={s.padded}><h2>让学习有据可查</h2><details className={s.resource}><summary>查看示例资料节选</summary><MarkdownView source={demoSource} /></details><p className={s.note}>示例不上传文件。开始自己的项目后，可以导入真实资料、查看引用，并选择生成复习或练习所需的资料范围。</p></section>}
        {step === 5 && <section className={s.padded}><h2>把一个知识点放回体系</h2><p className={s.note}>优化方法 · 示例地图</p>
          <details className={s.resource} open><summary>一阶优化 → 梯度下降</summary><p>目标：理解目标函数、梯度和学习率的关系。</p><ul><li>方向：局部变化与负梯度</li><li>步长：下降、振荡与发散</li><li>边界：凸性、驻点与停止条件</li></ul></details>
          <details className={s.resource}><summary>接下来可探索：随机梯度与动量</summary><p>当完整梯度计算昂贵时，如何使用采样？如何利用过去的更新信息？这些是后续主题，本次示例尚未讲解。</p></details>
          <p className={s.note}>你已经体验了从提问到复习、练习、笔记和资料的过程。创建自己的系统学习项目，或先建一张学科地图。</p>
          {onConfigure && <div className={s.annotation}><p>还没有 AI 连接？可在设置里添加已有的 API Key，也可查看不同服务商的获取入口。</p><Button size="sm" onClick={onConfigure}>设置 AI 连接</Button></div>}
        </section>}
      </div>
    </div>
  </Modal>;
}

function DemoReviewBody({ pageIndex, onSave }: { pageIndex: number; onSave: (quote: string) => void }) {
  const root = useRef<HTMLDivElement>(null);
  const [quote, setQuote] = useState('');
  const [answer, setAnswer] = useState(false);
  const [saved, setSaved] = useState(false);
  const select = (text: string) => { setQuote(text); setAnswer(false); setSaved(false); };
  const capture = () => {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !root.current || !selection.anchorNode || !selection.focusNode) return;
    if (root.current.contains(selection.anchorNode) && root.current.contains(selection.focusNode)) select(selection.toString().trim().slice(0, 1000));
  };
  return <section>
    <div onMouseUp={capture} onKeyUp={capture}><MarkdownView ref={root} source={demoReviewPages[pageIndex].source} /></div>
    <div className={s.annotationActions}><Button size="sm" onClick={() => select(pageIndex === 0 ? demoSentence : '不能从梯度小推出全局最优。')}>选择示例句</Button></div>
    {quote && <aside className={s.annotation}><blockquote>{quote}</blockquote><div className={s.annotationActions}><Button size="sm" onClick={() => { onSave(quote); setSaved(true); }}>{saved ? '已保存批注' : '保存批注'}</Button><Button size="sm" onClick={() => setAnswer(true)}>问 AI · 预置答疑</Button></div>
      {answer && <div role="status"><h3>预置答疑</h3><p>这里要区分局部判断和整体结论。梯度给出当前位置的变化方向；一步能否下降取决于步长，结果是否全局最优还取决于函数结构。可以把学习率改成 1，再检查二次函数的更新，看看这个区别。</p></div>}
    </aside>}
  </section>;
}
