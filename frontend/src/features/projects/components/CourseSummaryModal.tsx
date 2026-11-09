import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { http } from '@/shared/client';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import type { CourseLesson } from '../api/courseSummaries';

export function CourseSummaryModal({ mapSlug, mapVersionId, title, lessons, open, onOpenChange }: {
  mapSlug: string; mapVersionId: string; title: string; lessons: CourseLesson[]; open: boolean; onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const associate = useMutation({
    mutationFn: (lesson: CourseLesson) => http.post(`/api/projects/${encodeURIComponent(lesson.projectSlug)}/course-binding`, { mapSlug, mapVersionId, topicId: lesson.topicId }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['projects', mapSlug, 'course-summaries'] }),
  });
  const retry = useMutation({
    mutationFn: ({ slug, id }: { slug: string; id: string }) => http.post(`/api/projects/${encodeURIComponent(slug)}/teaching-summaries/${encodeURIComponent(id)}/retry`, {}),
    onSuccess: () => client.invalidateQueries({ queryKey: ['projects', mapSlug, 'course-summaries'] }),
  });
  return <Modal open={open} onOpenChange={onOpenChange} title={`${title} · 学习概括`} size="sm">
    {lessons.length === 0 && <p>还没有这个小节的学习记录。</p>}
    {lessons.map(lesson => <section key={lesson.projectSlug}>
      {!lesson.linked && <p role="status">旧学习记录尚不能可靠关联到当前地图，暂不会带入后续章节。</p>}
      {lesson.needsConfirmation && <p>确认这份学习记录属于当前地图的“{title}”后，可恢复后续章节衔接。<Button disabled={associate.isPending} onClick={() => associate.mutate(lesson)}>确认关联到当前地图</Button></p>}
      {lesson.error && <p role="alert">{lesson.error}</p>}
      {!lesson.error && !lesson.summaries?.length && <p>尚未生成概括。学完后在对话中确认“我学完了”，教师会归纳本次讲过的概念。</p>}
      {lesson.summaries?.map(summary => <div key={summary.id}>
        {summary.status === 'completed' ? <>
          <p>概念：{summary.concepts?.join('、') || '暂无'}</p>
          <p>场景：{summary.scenario || '无'}</p>
        </> : ['pending', 'running'].includes(summary.status) ? <p role="status">概括生成中…</p>
          : ['failed', 'interrupted'].includes(summary.status) ? <p>概括未生成成功。<Button disabled={retry.isPending} onClick={() => retry.mutate({ slug: lesson.projectSlug, id: summary.id })}>重试概括</Button></p>
            : <p>这份概括已失效或撤销，不会带入后续章节。</p>}
      </div>)}
      {!lesson.summaries?.some(summary => summary.status === 'completed') && <Button onClick={() => navigate(`/project/${lesson.projectSlug}/teacher`)}>返回学习</Button>}
    </section>)}
    {retry.error && <p role="alert">重试失败：{(retry.error as Error).message}</p>}
    {associate.error && <p role="alert">关联失败：{(associate.error as Error).message}</p>}
  </Modal>;
}
