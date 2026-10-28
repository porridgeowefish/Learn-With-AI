import { Link } from 'react-router-dom';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import faq from './faq.md?raw';
import s from './Treasures.module.css';
export function TreasureFAQ(){return <article className={s.faq}><Link to="/collection">← 返回收藏</Link><MarkdownView source={faq}/></article>;}
