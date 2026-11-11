import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { StarFilledIcon, StarIcon } from '@radix-ui/react-icons';
import { Button } from '@/shared/primitive/Button';
import { Modal } from '@/shared/primitive/Modal';
import { learningWorkspaceKeys, rateLesson } from '../api/learningWorkspace';
import s from './LessonRating.module.css';

export function LessonRating({ slug, messageId }: { slug: string; messageId: string }) {
  const [stars, setStars] = useState(0);
  const [saved, setSaved] = useState(false);
  const client = useQueryClient();
  const rating = useMutation({
    mutationFn: (score: number) => rateLesson(slug, messageId, score),
    onSuccess: () => {
      setSaved(true);
      void client.invalidateQueries({ queryKey: learningWorkspaceKeys.conversation(slug) });
    },
  });

  function submit(score: number) {
    if (!saved && !rating.isPending) rating.mutate(score);
  }

  return <Modal open={!saved} onOpenChange={open => { if (!open) submit(0); }} size="sm"
    title="本次教学效果如何？" description="请选择 1 到 5 星，也可以跳过。"
    footer={<>
      <Button variant="ghost" disabled={rating.isPending} onClick={() => submit(0)}>跳过</Button>
      <Button variant="primary" loading={rating.isPending} disabled={!stars} onClick={() => submit(stars)}>提交评分</Button>
    </>}>
    <div className={s.stars} role="group" aria-label="教学评分">
      {[1, 2, 3, 4, 5].map(value => <button key={value} type="button" aria-label={`${value} 星`}
        aria-pressed={stars === value} onClick={() => setStars(value)} disabled={rating.isPending}
        className={value <= stars ? s.selected : undefined}>
        {value <= stars ? <StarFilledIcon aria-hidden="true" /> : <StarIcon aria-hidden="true" />}
      </button>)}
    </div>
    {rating.isError && <p role="alert">评分未保存，请重试。</p>}
  </Modal>;
}
