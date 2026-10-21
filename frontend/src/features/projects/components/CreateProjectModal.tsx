import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { useCreateProject } from '@/features/projects/api/projects';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import { Icon } from '@/shared/primitive/Icon';
import type { ProjectType } from '@/shared/types/domain';
import type { LearningScopeSource } from '@/shared/types/api';
import { ProjectTypeAdvisorDialog } from './ProjectTypeAdvisorDialog';

import s from './CreateProjectModal.module.css';

const schema = z.object({
  projectType: z.enum(['discipline-map', 'system-learning'], {
    required_error: '请选择学科地图或系统学习',
    invalid_type_error: '请选择学科地图或系统学习',
  }),
  title: z.string().trim().min(1, '请填写项目标题'),
  systemPrompt: z.string().optional().default(''),
});

type FormValues = z.infer<typeof schema>;

interface CreateProjectModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (projectSlug: string) => void;
  fromDisciplineMap?: boolean;
  fixedProjectType?: ProjectType;
  initialTitle?: string;
  initialScopeSource?: LearningScopeSource;
}

export function CreateProjectModal({
  open,
  onOpenChange,
  onCreated,
  fromDisciplineMap = false,
  fixedProjectType,
  initialTitle = '',
  initialScopeSource,
}: CreateProjectModalProps) {
  const create = useCreateProject();
  const [advisorOpen, setAdvisorOpen] = useState(false);
  const {
    register,
    handleSubmit,
    reset,
    getValues,
    setValue,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      title: initialTitle,
      projectType: fixedProjectType,
      systemPrompt: '',
    },
  });

  // Re-seed on open so a topic selected from the map is actually prefilled.
  useEffect(() => {
    if (open) {
      reset({
        title: initialTitle,
        projectType: fixedProjectType,
        systemPrompt: '',
      });
    } else {
      setAdvisorOpen(false);
    }
  }, [fixedProjectType, initialTitle, open, reset]);

  const onSubmit = handleSubmit(async (values) => {
    const projectType = fromDisciplineMap ? 'system-learning' : values.projectType;
    const payload = projectType === 'discipline-map'
      ? { title: values.title, projectType }
      : { ...values, projectType, ...(initialScopeSource ? { scopeSource: initialScopeSource } : {}) };
    const res = await create.mutateAsync(payload);
    onOpenChange(false);
    onCreated?.(res.project.slug);
  });

  const selectedType = watch('projectType');

  const formId = fromDisciplineMap ? 'deep-dive-project-form' : 'create-project-form';
  const currentDraft = getValues();

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={fromDisciplineMap ? '创建深入学习项目' : '新建项目'}
      description={fromDisciplineMap ? '创建一个普通、平级的系统学习项目；之后仍可在侧栏自由分类' : '先选择当前需要的是建立地图，还是进入系统学习'}
      size="md"
      footer={(
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            loading={isSubmitting || create.isPending}
          >
            {fromDisciplineMap ? '确认创建' : '创建项目'}
          </Button>
        </>
      )}
    >
      <form data-tour="create-project-form" id={formId} className={s.form} onSubmit={onSubmit}>
        {!fixedProjectType && (
          <div className={s.field}>
            <div className={s.typeHeader}>
              <label>项目形态 <span className={s.req}>*</span></label>
              <button
                type="button"
                className={s.advisorTrigger}
                onClick={() => setAdvisorOpen(true)}
                aria-label="问问 AI，帮我选择项目形态"
              >
                <Icon name="bot" size={15} />
                <span>问问 AI</span>
                <small>选择顾问</small>
              </button>
            </div>
            <div className={s.typeGrid}>
              <label className={s.typeCard}>
                <input type="radio" value="discipline-map" {...register('projectType')} />
                <strong>学科地图</strong>
                <span>先全面了解一门学科的边界、领域与路线。</span>
              </label>
              <label className={s.typeCard}>
                <input type="radio" value="system-learning" {...register('projectType')} />
                <strong>系统学习</strong>
                <span>围绕具体主题进入引入、讲解、练习、拓展和总结。</span>
              </label>
            </div>
            {errors.projectType && <div className={s.error}>{errors.projectType.message}</div>}
          </div>
        )}
        <div className={s.field}>
          <label htmlFor="cp-title">
            项目标题 <span className={s.req}>*</span>
          </label>
          <input
            id="cp-title"
            type="text"
            placeholder="例如：Rust 所有权模型"
            autoComplete="off"
            {...register('title')}
          />
          <div className={s.hint}>我们会根据标题自动创建本地项目目录，后续仍可修改显示标题。</div>
          {errors.title && <div className={s.error}>{errors.title.message}</div>}
        </div>

        {selectedType === 'system-learning' && (
          <div className={s.field}>
            <label htmlFor="cp-instructions">有无特殊要求？（选填）</label>
            <textarea id="cp-instructions" rows={3} placeholder="例如：先讲直觉，再讲公式；多用生活中的例子。可以不填。" {...register('systemPrompt')} />
            <div className={s.hint}>附加信息会保存在当前对话中，后续回答持续使用。开始学习后，可随时在“附加信息”中修改或清空。</div>
          </div>
        )}

        {create.error && (
          <div className={s.error}>创建失败：{(create.error as Error).message}</div>
        )}

      </form>
      {!fixedProjectType && (
        <ProjectTypeAdvisorDialog
          open={advisorOpen}
          onOpenChange={setAdvisorOpen}
          draft={{
            title: currentDraft.title,
            why: currentDraft.systemPrompt,
          }}
          onAdopt={(projectType) => setValue('projectType', projectType, { shouldValidate: true })}
        />
      )}
    </Modal>
  );
}
