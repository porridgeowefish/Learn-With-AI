import { Link } from 'react-router-dom';
import clsx from 'clsx';

import type { ProjectMeta } from '@/shared/types/domain';
import { Icon } from '@/shared/primitive/Icon';
import { Tag } from '@/shared/primitive/Tag';

import s from './ProjectCard.module.css';

interface ProjectCardProps {
  project?: ProjectMeta;
  variant?: 'tile' | 'create';
  className?: string;
  onClick?: () => void;
}

export function ProjectCard({ project, variant = 'tile', className, onClick }: ProjectCardProps) {
  if (variant === 'create') {
    return (
      <div
        className={clsx(s.card, s.create, className)}
        role="button"
        tabIndex={0}
        onClick={onClick}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onClick?.();
          }
        }}
      >
        <Icon name="plus" size={20} className={s.plus} />
        <span className={s.title}>新建项目</span>
      </div>
    );
  }

  if (!project) return null;

  return (
    <Link to={`/project/${project.slug}`} className={clsx(s.card, className)}>
      <div className={s.head}>
        <span className={s.letter}>{project.title.slice(0, 1).toUpperCase()}</span>
        <h4 className={s.title}>{project.title}</h4>
      </div>
      <div className={s.meta}>
        <code className={s.slug}>{project.slug}</code>
 {project.layoutStatus && project.layoutStatus !== "current" && <Tag tone="muted">{project.layoutStatus === "legacy-format-unsupported" ? "旧版格式 · 需手动处理" : "数据需检查"}</Tag>}
        <Tag tone={project.projectType === 'discipline-map' ? 'orange' : 'muted'}>
          {project.projectType === 'discipline-map' ? '学科地图' : '系统学习'}
        </Tag>
      </div>
    </Link>
  );
}
