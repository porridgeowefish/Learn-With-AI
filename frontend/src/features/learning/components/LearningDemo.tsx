import { useEffect, useRef, useState } from 'react';
import { Button } from '@/shared/primitive/Button';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { Modal } from '@/shared/primitive/Modal';
import { PracticeReader } from './PracticeReader';
import { ReviewDocument } from './ReviewDocument';
import { demoNotes, demoPractice, demoQuestions, demoReview, demoSentence } from './learningDemoContent';
import s from './LearningDemo.module.css';

const steps = [
  { title: '教师对话', instruction: '选一个预置问题，看看老师如何把直觉、计算和边界连起来。真实学习时，在教师页输入自己的问题即可。' },
  { title: '复习与批注', instruction: '阅读这篇复习稿。选中一句话保存批注，或点“选择示例句”试一次。' },
  { title: '练习', instruction: '先尝试作答，再展开“参考答案与解析”。真实项目可生成新一轮练习，并翻看以前的轮次。' },
  { title: '笔记', instruction: '在这里写下自己的理解。真实项目的笔记与复习页也有笔记，自动笔记和自己的复习批注各自保留。' },
  { title: '附件', instruction: '真实学习时，可在对话栏上传附件，也可进入“附件”管理文件。教师回答会列出查阅过的资料和范围。' },
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
          <DemoReviewBody onSave={quote => setAnnotations(current => current.includes(quote) ? current : [...current, quote])} />
          {annotations.length > 0 && <aside className={s.saved}><h3>本次批注 · {annotations.length}</h3>{annotations.map(quote => <blockquote key={quote}>{quote}</blockquote>)}</aside>}
        </div>}
        <div hidden={step !== 2} className={s.padded}><p className={s.note}>临时作答：提交后自动判分，示例成绩不会保存。</p><PracticeReader content={demoPractice} versionId="guided-example-v1" /></div>
        {step === 3 && <section className={s.padded}><label className={s.noteField}>我的示例笔记<textarea value={notes} onChange={event => setNotes(event.target.value)} /></label><p className={s.note} role="status">修改会在本次体验中保留。</p>{annotations.length > 0 && <aside className={s.saved}><h3>刚才保存的复习批注</h3>{annotations.map(quote => <blockquote key={quote}>{quote}</blockquote>)}</aside>}</section>}
        {step === 4 && <section className={s.padded}><h2>本次查阅来源</h2><p>1 · 一元二次函数 · 第 1–2 段</p><p className={s.note}>开始自己的项目后，可导入资料供教师查阅，也可选择生成复习稿和练习时要使用的资料。</p></section>}
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

function DemoReviewBody({ onSave }: { onSave: (quote: string) => void }) {
  const [quote, setQuote] = useState('');
  const [saved, setSaved] = useState(false);
  const select = (text: string) => { setQuote(text); setSaved(false); };
  return <section>
    <ReviewDocument content={demoReview} onSelection={selection => { if (selection) select(selection.text.slice(0, 1000)); }} />
    <div className={s.annotationActions}><Button size="sm" onClick={() => select(demoSentence)}>选择示例句</Button></div>
    {quote && <aside className={s.annotation}><blockquote>{quote}</blockquote><div className={s.annotationActions}><Button size="sm" onClick={() => { onSave(quote); setSaved(true); }}>{saved ? '已保存批注' : '保存批注'}</Button></div></aside>}
  </section>;
}
