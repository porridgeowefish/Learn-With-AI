import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { readSource, type TreasureReward } from './api';
import s from './Treasures.module.css';

export function TreasureSourceLink({ reward }: { reward: TreasureReward }) {
  const navigate = useNavigate();
  const [checking, setChecking] = useState(false);
  const source = useQuery({ queryKey: ['treasure-source', reward.id], queryFn: () => readSource(reward.id), staleTime: 0 });

  async function open() {
    setChecking(true);
    try {
      const result = await source.refetch();
      if (result.data?.available) navigate(result.data.url);
    } finally { setChecking(false); }
  }

  return <div className={s.source}>
    <small>{new Date(reward.createdAt).toLocaleDateString()} · {reward.kind === 'feynman' ? '讲给教师听' : '教师对话'}{reward.redeemed ? ' · 已换额度' : ''}</small>
    <button type="button" disabled={checking || source.isLoading || source.data?.available === false} onClick={() => void open()}>
      {source.isError ? '来源读取失败，点击重试' : source.data?.available === false ? '原对话已不可用' : `${reward.projectTitle} ↗`}
    </button>
    {source.data?.available === false && <small>收藏记录仍为你保留</small>}
  </div>;
}
