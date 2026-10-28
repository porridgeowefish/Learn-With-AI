import { useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useProjectStore, useUiStore } from '@/shared/store';
import { ChevronDownIcon, PlusIcon } from '@radix-ui/react-icons';

import { useDeleteProject, useProjects } from '@/features/projects/api/projects';
import { useFolders, useSaveFolders } from '@/features/projects/api/folders';
import {
  assignProject,
  createFolder,
  deleteFolder,
  folderOf,
  renameFolder,
  uncategorizedSlugs,
  type FolderLayout,
} from '@/features/projects/lib/folders';
import { Icon, type IconName } from '@/shared/primitive/Icon';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import { clearDeletedProjectClientData } from '@/features/projects/lib/projectDeletion';

import s from './Sidebar.module.css';
import { UpdateNotice } from './UpdateNotice';

const QUICK_LINKS: ReadonlyArray<{ to: string; label: string; icon: IconName }> = [
  { to: '/', label: '主页', icon: 'zap' },
  { to: '/collection', label: '我的收藏', icon: 'circle' },
  { to: '/usage', label: 'Token 用量', icon: 'clock' },
  { to: '/preferences', label: '学习偏好', icon: 'brain' },

  { to: '/settings', label: '设置', icon: 'terminal' },
];

export function Sidebar() {
  const location = useLocation();
  const navigate = useNavigate();
  const selectedProjectId = useProjectStore((state) => state.selectedProjectId);
  const selectProject = useProjectStore((state) => state.selectProject);
  const { data: projectsData, isLoading } = useProjects();
  const projects = projectsData?.projects ?? [];
  const { data: foldersData } = useFolders();
  const layout: FolderLayout = foldersData ?? { folders: [] };
  const save = useSaveFolders();
  const deleteProject = useDeleteProject();

  // Discipline maps are rendered through the same folder header model below;
  // only concrete system-learning projects become child rows.
  const allSlugs = projects
    .filter((project) => project.projectType === 'system-learning')
    .map((project) => project.slug);
  const uncategorized = uncategorizedSlugs(layout, allSlugs);
  const projectBySlug = new Map(projects.map((p) => [p.slug, p] as const));

  // Collapsed folders are a persisted UI pref (ui Zustand slice → localStorage),
  // so expand/collapse choices survive reload. Default: all expanded.
  const collapsedFolderIds = useUiStore((st) => st.collapsedFolderIds);
  const toggleFolderCollapsed = useUiStore((st) => st.toggleFolderCollapsed);
  const collapsed = new Set(collapsedFolderIds);

  const [creating, setCreating] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [menuSlug, setMenuSlug] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<
    | { kind: 'folder'; id: string; title: string }
    | { kind: 'project'; slug: string; title: string; projectType: 'discipline-map' | 'system-learning' }
    | null
  >(null);

  const commit = (next: FolderLayout) => save.mutate(next);

  const submitCreate = () => {
    const name = draftName.trim();
    if (name) commit(createFolder(layout, name));
    setDraftName('');
    setCreating(false);
  };

  const submitRename = () => {
    if (renamingId) commit(renameFolder(layout, renamingId, renameValue));
    setRenamingId(null);
    setRenameValue('');
  };

  const removeFolder = (id: string, name: string) => {
    setDeleteTarget({ kind: 'folder', id, title: name });
  };

  const moveProject = (slug: string, target: string | null) => {
    commit(assignProject(layout, slug, target));
  };

  const removeProject = (slug: string, title: string) => {
    const project = projectBySlug.get(slug);
    if (!project) return;
    setDeleteTarget({ kind: 'project', slug, title, projectType: project.projectType });
  };

  const confirmDelete = () => {
    if (!deleteTarget) return;
    if (deleteTarget.kind === 'folder') {
      commit(deleteFolder(layout, deleteTarget.id));
      setDeleteTarget(null);
      return;
    }
    const { slug } = deleteTarget;
    deleteProject.mutate(slug, {
      onSuccess: () => {
        clearDeletedProjectClientData(slug);
        setMenuSlug(null);
        setDeleteTarget(null);
        if (selectedProjectId === slug) {
          selectProject(null);
        }
        if (location.pathname.startsWith(`/project/${slug}`)) navigate('/');
      },
    });
  };

  const renderProject = (slug: string) => {
    const p = projectBySlug.get(slug);
    if (!p) return null;
    const currentFolderId = folderOf(layout, p.slug);
    return (
      <div key={p.slug}>
        <ProjectRow
          slug={p.slug}
          title={p.title}
          projectType={p.projectType}
          currentFolderId={currentFolderId}
          folders={layout.folders}
          menuOpen={menuSlug === p.slug}
          onToggleMenu={() => setMenuSlug(menuSlug === p.slug ? null : p.slug)}
          onCloseMenu={() => setMenuSlug(null)}
          onMove={(target) => moveProject(p.slug, target)}
          onDelete={() => removeProject(p.slug, p.title)}
        />
      </div>
    );
  };

  return (
    <aside className={s.sidebar} aria-label="应用导航"><Link to="/" className={s.brand}><img src="/app-icon.png" alt=""/>Learn-With-AI</Link>
      <div className={s.head}>
        <span>导航</span>
      </div>
      <div className={s.scroll}>
        <div className={s.section}>快捷入口</div>
        {QUICK_LINKS.map((q) => (
          <Link key={q.to} to={q.to} className={s.link}>
            <Icon name={q.icon} size={15} className={s.linkIcon} />
            {q.label}
          </Link>
        ))}

        <div className={s.sectionRow}>
          <div className={s.section}>项目</div>
          <button
            type="button"
            className={s.iconBtn}
            title="新建文件夹"
            onClick={() => setCreating(true)}
          >
            <PlusIcon />
          </button>
        </div>

        {save.isPending && <div className={s.saving}>保存中…</div>}
        {save.isError && (
          <div className={s.saveError} role="alert">
            文件夹保存失败：{(save.error as Error).message}（请确认后端已重启）
          </div>
        )}
        {deleteProject.isError && (
          <div className={s.saveError} role="alert">
            删除失败：{(deleteProject.error as Error).message}
          </div>
        )}

        {isLoading && <div className={s.muted}>加载中…</div>}
        {!isLoading && projects.length === 0 && (
          <div className={s.muted}>尚无项目，去总览创建一个 →</div>
        )}

        {layout.folders.map((f) => {
          const isOpen = !collapsed.has(f.id);
          const isRenaming = renamingId === f.id;
          return (
            <div key={f.id} className={s.folder}>
              <div className={s.folderHeader}>
                <button
                  type="button"
                  className={s.chevronBtn}
                  onClick={() => toggleFolderCollapsed(f.id)}
                  aria-label={isOpen ? '收起' : '展开'}
                >
                  <ChevronDownIcon className={isOpen ? undefined : s.chevronClosed} />
                </button>
                {isRenaming ? (
                  <input
                    className={s.folderInput}
                    autoFocus
                    value={renameValue}
                    onChange={(e) => setRenameValue(e.target.value)}
                    onBlur={submitRename}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') submitRename();
                      if (e.key === 'Escape') {
                        setRenamingId(null);
                        setRenameValue('');
                      }
                    }}
                  />
                ) : (
                  <button
                    type="button"
                    className={s.folderName}
                    title="单击收起/展开 · 双击重命名"
                    onClick={() => toggleFolderCollapsed(f.id)}
                    onDoubleClick={() => {
                      setRenamingId(f.id);
                      setRenameValue(f.name);
                    }}
                  >
                    <span className={s.folderNameText}>{f.name}</span>
                    <span className={s.count}>{f.slugOrder.length}</span>
                  </button>
                )}
                {!f.mapProjectSlug && (
                  <button
                    type="button"
                    className={s.iconBtn}
                    title="删除文件夹"
                    onClick={() => removeFolder(f.id, f.name)}
                  >
                    <Icon name="x" size={13} />
                  </button>
                )}
              </div>
              {isOpen && (
                <div className={s.folderBody}>
                  {f.mapProjectSlug && (() => {
                    const mapProject = projectBySlug.get(f.mapProjectSlug);
                    if (!mapProject) return null;
                    return (
                      <ProjectRow
                        slug={mapProject.slug}
                        title={`${f.name}学科总览`}
                        projectType="discipline-map"
                        currentFolderId={f.id}
                        folders={layout.folders}
                        menuOpen={menuSlug === mapProject.slug}
                        onToggleMenu={() => setMenuSlug(menuSlug === mapProject.slug ? null : mapProject.slug)}
                        onCloseMenu={() => setMenuSlug(null)}
                        onMove={() => undefined}
                        onDelete={() => removeProject(mapProject.slug, mapProject.title)}
                      />
                    );
                  })()}
                  {f.slugOrder.map((slug) => renderProject(slug))}
                </div>
              )}
            </div>
          );
        })}

        {uncategorized.length > 0 && layout.folders.length > 0 && (
          <div className={s.subSection}>未分类</div>
        )}
        {uncategorized.map((slug) => renderProject(slug))}

        {creating && (
          <input
            className={`${s.folderInput} ${s.createInput}`}
            autoFocus
            placeholder="文件夹名称，回车创建"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            onBlur={submitCreate}
            onKeyDown={(e) => {
              if (e.key === 'Enter') submitCreate();
              if (e.key === 'Escape') {
                setCreating(false);
                setDraftName('');
              }
            }}
          />
        )}
      </div>
      <div className={s.foot}><UpdateNotice/><div className={s.utilities}><a href="https://github.com/porridgeowefish/Learn-With-AI" target="_blank" rel="noreferrer" aria-label="在 GitHub 查看 Learn-With-AI 源码">GitHub</a></div></div>
      <Modal
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={deleteTarget?.kind === 'folder'
          ? '删除文件夹？'
          : deleteTarget?.projectType === 'discipline-map'
            ? '删除学科项目？'
            : '删除学习单元？'}
        description={deleteTarget?.kind === 'folder'
          ? '只删除侧栏分类，其中的学习内容会回到「未分类」。'
          : '这个操作会永久删除本地文件、学习记录和相关会话，无法撤销。'}
        size="sm"
        footer={(
          <>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>取消</Button>
            <Button variant="danger" loading={deleteProject.isPending} onClick={confirmDelete}>
              确认删除
            </Button>
          </>
        )}
      >
        <div className={s.deleteSummary}>
          <Icon name="x" size={18} />
          <div>
            <strong>{deleteTarget?.title}</strong>
            <span>{deleteTarget?.kind === 'folder' ? '学习内容不会被删除' : '删除后不能恢复'}</span>
          </div>
        </div>
      </Modal>
    </aside>
  );
}

interface ProjectRowProps {
  slug: string;
  title: string;
  projectType: 'discipline-map' | 'system-learning';
  currentFolderId: string | null;
  folders: ReadonlyArray<{ id: string; name: string }>;
  menuOpen: boolean;
  onToggleMenu: () => void;
  onCloseMenu: () => void;
  onMove: (folderId: string | null) => void;
  onDelete: () => void;
}

function ProjectRow({
  slug,
  title,
  projectType,
  currentFolderId,
  folders,
  menuOpen,
  onToggleMenu,
  onCloseMenu,
  onMove,
  onDelete,
}: ProjectRowProps) {
  const btnRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [menuPos, setMenuPos] = useState<CSSProperties | null>(null);

  // The menu is position: fixed so it escapes the sidebar's overflow clipping,
  // but that means we place it ourselves. On open, measure the trigger and the
  // menu, flip up if it would overflow the viewport bottom, and clamp into the
  // viewport. useLayoutEffect runs before paint, so there's no flash at the
  // wrong position.
  useLayoutEffect(() => {
    if (!menuOpen) {
      setMenuPos(null);
      return;
    }
    const btn = btnRef.current;
    const menu = menuRef.current;
    if (!btn || !menu) return;
    const r = btn.getBoundingClientRect();
    const m = menu.getBoundingClientRect();
    const PAD = 6;
    let top = r.bottom + 4;
    if (top + m.height > window.innerHeight - PAD) top = r.top - m.height - 4;
    if (top < PAD) top = PAD;
    let left = r.left;
    if (left + m.width > window.innerWidth - PAD) left = window.innerWidth - m.width - PAD;
    if (left < PAD) left = PAD;
    setMenuPos({ top, left });
  }, [menuOpen]);

  return (
    <div className={s.projectRow}>
      <Link to={`/project/${slug}`} className={s.link}>
        <Icon name="folder" size={15} className={s.linkIcon} />
        <span className={s.title}>{title}</span>
        <span className={s.projectType}>{projectType === 'discipline-map' ? '总览' : '学习'}</span>
      </Link>
      <div className={s.menuWrap}>
        <button
          ref={btnRef}
          type="button"
          className={`${s.menuBtn} ${menuOpen ? s.menuBtnOpen : ''}`}
          title="项目操作"
          aria-label={`项目操作：${title}`}
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onToggleMenu();
          }}
        >
          ⋯
        </button>
        {menuOpen && (
          <>
            <div className={s.menuBackdrop} onClick={onCloseMenu} />
            <div ref={menuRef} className={s.menu} role="menu" style={menuPos ?? undefined}>
              {projectType === 'system-learning' && (
                <>
                  <div className={s.menuLabel}>移动到文件夹</div>
                  <button
                    type="button"
                    role="menuitem"
                    className={currentFolderId === null ? s.menuActive : ''}
                    onClick={() => {
                      onMove(null);
                      onCloseMenu();
                    }}
                  >
                    未分类
                  </button>
                  {folders.map((f) => (
                    <button
                      key={f.id}
                      type="button"
                      role="menuitem"
                      className={currentFolderId === f.id ? s.menuActive : ''}
                      onClick={() => {
                        onMove(f.id);
                        onCloseMenu();
                      }}
                    >
                      {f.name}
                    </button>
                  ))}
                  <div className={s.menuDivider} />
                </>
              )}
              <button
                  type="button"
                  role="menuitem"
                  className={s.menuDanger}
                  onClick={onDelete}
              >
                {projectType === 'discipline-map' ? '删除学科项目' : '删除学习单元'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
