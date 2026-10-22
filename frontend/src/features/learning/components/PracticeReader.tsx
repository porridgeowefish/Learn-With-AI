import { useMemo, useState } from 'react';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import type { PracticeResult } from '../api/practiceResults';
import { choiceLabel, expectedAnswer, gradeTest, type GradedTest } from './practiceGrading';
import s from './AssetsView.module.css';

export interface PracticeQuestion { question: string; answer: string; kind?: string; options?: Array<{id:string;text:string}> }
export const withoutPracticeTitle = (source:string) => source.replace(/^\s*#\s+(?:练习|练习题)[ \t]*(?:\r?\n|$)/, '');
export function parsePractice(source: string): {intro:string;questions:PracticeQuestion[]} | null {
 const lines=source.split(/\r?\n/), boundaries:number[]=[], answers=new Map<number,number>(), explanations=new Map<number,number>();
 let fence: {char:string;length:number}|null=null, math=false;
 const structural=new Set<number>();
 for(let i=0;i<lines.length;i++){
  const marker=lines[i].match(/^ {0,3}(`{3,}|~{3,})/);
  if(marker){if(!fence)fence={char:marker[1][0],length:marker[1].length};else if(marker[1][0]===fence.char&&marker[1].length>=fence.length)fence=null;continue;}
  if(fence)continue;
  if(lines[i].trim().startsWith('$$')){if((lines[i].match(/\$\$/g)??[]).length===1)math=!math;continue;}
  if(math)continue;
  structural.add(i);
  const heading=lines[i].match(/^##\s+第\s*(\d+)\s*题\s*$/);
  if(heading){if(Number(heading[1])!==boundaries.length+1)return null;boundaries.push(i);}
  if(/^###\s+参考答案\s*$/.test(lines[i])&&boundaries.length)answers.set(boundaries.length-1,i);
  if(/^###\s+解析\s*$/.test(lines[i])&&boundaries.length)explanations.set(boundaries.length-1,i);
 }
 if(!boundaries.length)return null;
 const questions:PracticeQuestion[]=[];
 for(let n=0;n<boundaries.length;n++){
  const start=boundaries[n],end=boundaries[n+1]??lines.length,answer=answers.get(n),explanation=explanations.get(n);
  if(answer===undefined||explanation===undefined||explanation<=answer||explanation>=end)return null;
  if(!lines.slice(answer+1,explanation).join('\n').trim()||!lines.slice(explanation+1,end).join('\n').trim())return null;
  const answerText=lines.slice(answer,end).join('\n');
  const questionLines=lines.slice(start,answer),kind=questionLines.find((_,i)=>structural.has(start+i)&&/题型：/.test(questionLines[i]))?.match(/题型：\s*([a-z-]+)/)?.[1];
  const options:Array<{id:string;text:string}>=[];
  const body=questionLines.filter((line,i)=>{
   const match=structural.has(start+i)&&line.match(/^([A-Z])[.、．)]\s*(.+)$/);
   if(match&&(kind==='single-choice'||kind==='multiple-choice')){options.push({id:match[1],text:match[2]});return false;}
   return true;
  });
  questions.push({question:body.join('\n'),answer:answerText,kind,options});
 }
 return {intro:withoutPracticeTitle(lines.slice(0,boundaries[0]).join('\n')),questions};
}
const KIND_LABELS:Record<string,string>={'single-choice':'单选题','multiple-choice':'多选题','true-false':'判断题','short-answer':'简答题',essay:'论述题',code:'代码题'};
/** Show the metadata line as its own readable paragraph instead of letting
 * Markdown join it to the stem ("题型：single-choice · 难度：1/5 题干…"). */
export function readableQuestion(source:string){
 return source.replace(/^题型：\s*([a-z-]+)\s*·\s*难度：\s*([1-5])\/5[ \t]*$/m,(_m,kind:string,level:string)=>`${KIND_LABELS[kind]??kind} · 难度 ${level}/5\n`);
}
function optionsOf(question:PracticeQuestion){
 return question.options?.length?question.options:question.kind==='true-false'?[{id:'true',text:'正确'},{id:'false',text:'错误'}]:[];
}
function Exercise({question,index,selected,onSelect,mark,locked}:{question:PracticeQuestion;index:number;selected:string[];onSelect:(next:string[])=>void;mark?:{correct:boolean;expected:string[]};locked:boolean}){
 const [draft,setDraft]=useState('');
 const options=optionsOf(question);
 const gradable=expectedAnswer(question)!==null;
 const multiple=question.kind==='multiple-choice'||(!question.kind&&(expectedAnswer(question)?.length??0)>1);
 return <section className={s.exercise} data-result={mark?(mark.correct?'correct':'wrong'):undefined}><MarkdownView source={readableQuestion(question.question)}/>
  {options.length?<fieldset className={s.optionList} disabled={locked}><legend>第{index+1}题作答{multiple?'（多选）':''}</legend>{options.map(option=>{
   const key=mark?.expected.includes(option.id)?'key':undefined;
   return <label key={option.id} className={s.option} data-key={mark?key:undefined} data-picked={selected.includes(option.id)||undefined}><input type={multiple?'checkbox':'radio'} name={`practice-${index}`} checked={selected.includes(option.id)} onChange={event=>onSelect(multiple?(event.target.checked?[...selected,option.id]:selected.filter(id=>id!==option.id)):[option.id])}/><span><MarkdownView source={`${option.id==='true'||option.id==='false'?'':`${option.id}. `}${option.text}`}/></span></label>;
  })}</fieldset>:<label className={s.practiceField}>我的作答<textarea aria-label={`第${index+1}题作答`} value={draft} onChange={event=>setDraft(event.target.value)}/></label>}
  {mark&&<p className={mark.correct?s.verdictRight:s.verdictWrong} role="status">{mark.correct?'✓ 回答正确':`✗ 回答错误 · 正确答案：${choiceLabel(question,mark.expected)}`}</p>}
  {gradable&&!mark?<p className={s.answerHidden}>提交测试后显示答案与解析</p>:<details className={s.answer}><summary>参考答案与解析</summary><MarkdownView source={question.answer}/></details>}
 </section>;
}

export interface PracticeTestHandlers {
 /** Saved result of this round, when persistence is available. */
 result?: PracticeResult;
 onSubmit?: (graded: GradedTest) => Promise<unknown>;
 onRetake?: () => Promise<unknown>;
}

/** Objective rounds are a test: answers are scored by the round's own key
 * once submitted, and the score stays until the learner chooses 重新测试.
 * Without handlers (guided demo) the score lives only in memory. */
export function PracticeReader({content,versionId,result,onSubmit,onRetake}:{content:string;versionId:string}&PracticeTestHandlers) {
 const parsed=useMemo(()=>parsePractice(content),[content]);
 if(!parsed)return <MarkdownView source={withoutPracticeTitle(content)} className={s.markdown}/>;
 return <PracticeTest key={versionId} intro={parsed.intro} questions={parsed.questions} versionId={versionId} result={result} onSubmit={onSubmit} onRetake={onRetake}/>;
}

function PracticeTest({intro,questions,versionId,result,onSubmit,onRetake}:{intro:string;questions:PracticeQuestion[];versionId:string}&PracticeTestHandlers){
 const [selections,setSelections]=useState<Record<string,string[]>>({});
 const [local,setLocal]=useState<GradedTest|null>(null);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState('');
 const graded:GradedTest|null=result??local;
 const gradableCount=useMemo(()=>questions.filter(question=>expectedAnswer(question)!==null).length,[questions]);
 const answered=questions.filter((question,index)=>expectedAnswer(question)!==null&&(selections[String(index+1)]?.length??0)>0).length;
 const submit=async()=>{
  const next=gradeTest(questions,selections);
  if(!next)return;
  setError('');setLocal(next);
  if(!onSubmit)return;
  setBusy(true);
  try{await onSubmit(next);}catch{setError('成绩已在本页判出，但保存失败。请检查服务后点“再次保存”。');}finally{setBusy(false);}
 };
 const retake=async()=>{
  setError('');
  if(onRetake&&result){setBusy(true);try{await onRetake();}catch{setError('重新测试失败：旧成绩未能清除，请稍后重试。');setBusy(false);return;}setBusy(false);}
  setLocal(null);setSelections({});
 };
 const shown=graded?graded.answers:selections;
 const percent=graded?Math.round(graded.score/graded.total*100):0;
 return <div className={s.practice}>
  {intro.trim()&&<MarkdownView source={intro}/>}
  {graded&&<div className={s.scoreCard} role="status" aria-label="测试成绩"><div><strong>得分 {graded.score} / {graded.total}</strong><span>{percent}%{result?.submittedAt?` · 已保存 ${new Date(result.submittedAt).toLocaleString('zh-CN',{hour12:false})}`:onSubmit?' · 未保存':' · 示例不保存'}</span></div>
   <div className={s.scoreActions}>{onSubmit&&!result&&<button type="button" onClick={()=>void onSubmit(graded).then(()=>setError('')).catch(()=>setError('保存仍然失败，请稍后再试。'))} disabled={busy}>再次保存</button>}<button type="button" onClick={()=>void retake()} disabled={busy}>重新测试</button></div></div>}
  {error&&<div className={s.error} role="alert">{error}</div>}
  <div className={s.practiceList}>{questions.map((question,index)=>{const id=String(index+1);return <Exercise question={question} index={index} key={`${versionId}-${index}`} selected={shown[id]??[]} mark={graded?.items[id]} locked={Boolean(graded)} onSelect={next=>setSelections(current=>({...current,[id]:next}))}/>;})}</div>
  {!graded&&gradableCount>0&&<div className={s.submitBar}><span>{answered<gradableCount?`已答 ${answered}/${gradableCount} 题，未答按错误计`:`已答全部 ${gradableCount} 题`}</span><button type="button" className={s.primary} onClick={()=>void submit()} disabled={busy||answered===0}>提交测试</button></div>}
 </div>;
}
