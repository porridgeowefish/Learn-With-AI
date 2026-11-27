import { useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { Link, matchPath, useLocation, useNavigate } from 'react-router-dom';
import { useProjectStore, useUiStore } from '@/shared/store';
import { ChevronDownIcon, PlusIcon } from '@radix-ui/react-icons';

import { useDeleteProject, useProjects } from '@/features/projects/api/projects';
import { useDeleteFolder, useFolders, useSaveFolders } from '@/features/projects/api/folders';
import {
  assignProject,
  createFolder,
  folderOf,
  renameFolder,
  uncategorizedSlugs,
  type Folder,
  type FolderLayout,
} from '@/features/projects/lib/folders';
import { Icon, type IconName } from '@/shared/primitive/Icon';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import { clearDeletedProjectClientData } from '@/features/projects/lib/projectDeletion';

import s from './Sidebar.module.css';
import { CloudAccount } from './CloudAccount';

const QUICK_LINKS: ReadonlyArray<{ to: string; label: string; icon: IconName }> = [
  { to: '/', label: '主页', icon: 'zap' },
  { to: '/learning-calendar', label: '学习日历', icon: 'clock' },
  { to: '/collection', label: '我的收藏', icon: 'circle' },
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
  const deleteFolder = useDeleteFolder();
  const deleting = deleteProject.isPending || deleteFolder.isPending;

  const folderTopics = new Set(layout.folders.map(folder => folder.mapProjectSlug));
  const allSlugs = projects.filter(project => !folderTopics.has(project.slug)).map(project => project.slug);
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
  const composingFolderName = useRef(false);
  const [menuSlug, setMenuSlug] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<
    | { kind: 'folder'; id: string; title: string; projectSlugs: string[] }
    | { kind: 'project'; slug: string; title: string; projectType: 'discipline-map' | 'system-learning' }
    | null
  >(null);

  const commit = (next: FolderLayout) => save.mutate(next);

  const submitCreate = () => {
    composingFolderName.current = false;
    const name = draftName.trim();
    if (name) commit(createFolder(layout, name));
    setDraftName('');
    setCreating(false);
  };

  const submitRename = () => {
    composingFolderName.current = false;
    if (renamingId) commit(renameFolder(layout, renamingId, renameValue));
    setRenamingId(null);
    setRenameValue('');
  };

  const removeFolder = (folder: Folder) => {
    deleteFolder.reset();
    const projectSlugs = [...folder.slugOrder];
    if (folder.mapProjectSlug) projectSlugs.push(folder.mapProjectSlug);
    setDeleteTarget({ kind: 'folder', id: folder.id, title: folder.name, projectSlugs });
  };

  const moveProject = (slug: string, target: string | null) => {
    commit(assignProject(layout, slug, target));
  };

  const removeProject = (slug: string, title: string) => {
    const project = projectBySlug.get(slug);
    if (!project) return;
    deleteProject.reset();
    setDeleteTarget({ kind: 'project', slug, title, projectType: project.projectType });
  };

  const finishDelete = (slugs: string[]) => {
    slugs.forEach(clearDeletedProjectClientData);
    setMenuSlug(null);
    setDeleteTarget(null);
    if (selectedProjectId && slugs.includes(selectedProjectId)) selectProject(null);
    const currentSlug = matchPath('/project/:slug/*', location.pathname)?.params.slug;
    if (currentSlug && slugs.includes(currentSlug)) navigate('/');
  };

  const confirmDelete = () => {
    if (!deleteTarget || deleting) return;
    if (deleteTarget.kind === 'folder') {
      const { id, projectSlugs } = deleteTarget;
      deleteFolder.mutate({ id, projectSlugs }, {
        onSuccess: ({ deletedProjectSlugs }) => finishDelete(deletedProjectSlugs),
      });
      return;
    }
    const { slug } = deleteTarget;
    deleteProject.mutate(slug, {
      onSuccess: () => finishDelete([slug]),
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
      <div className={s.scroll}>
        <div className={s.section}>快捷入口</div>
        {QUICK_LINKS.map((q) => (
          <Link key={q.to} to={q.to} className={s.link} aria-current={location.pathname === q.to ? 'page' : undefined}>
            <Icon name={q.icon} size={19} className={s.linkIcon} />
            {q.label}
          </Link>
        ))}
        <div className={s.sectionRow}>
          <div className={s.section}>学习库</div>
          <button
            type="button"
            className={s.iconBtn}
            title="新建分组"
            onClick={() => setCreating(true)}
          >
            <PlusIcon />
          </button>
        </div>

        {save.isPending && <div className={s.saving}>保存中…</div>}
        {save.isError && (
          <div className={s.saveError} role="alert">
            分组保存失败：{(save.error as Error).message}（请确认后端已重启）
          </div>
        )}
        {deleteFolder.data?.error && (
          <div className={s.saveError} role="alert">
            {deleteFolder.data.deletedProjectSlugs.length > 0 && `已删除 ${deleteFolder.data.deletedProjectSlugs.length} 个项目。`}
            {deleteFolder.data.error}
          </div>
        )}

        {isLoading && <div className={s.muted}>加载中…</div>}
        {!isLoading && projects.length === 0 && (
          <div className={s.muted}>还没有学习内容，可在首页和学习助手讨论。</div>
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
                    onCompositionStart={() => { composingFolderName.current = true; }}
                    onCompositionEnd={() => { composingFolderName.current = false; }}
                    onBlur={submitRename}
                    onKeyDown={(e) => {
                      if (composingFolderName.current || e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return;
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
                <button
                  type="button"
                  className={s.iconBtn}
                  title="删除分组"
                  disabled={save.isPending}
                  onClick={() => removeFolder(f)}
                >
                  <Icon name="x" size={13} />
                </button>
              </div>
              {isOpen && (
                <div className={s.folderBody}>
                  {f.mapProjectSlug && (() => {
                    const mapProject = projectBySlug.get(f.mapProjectSlug);
                    if (!mapProject) return null;
                    return (
                      <ProjectRow
                        slug={mapProject.slug}
                        title={mapProject.title}
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
            placeholder="分组名称，回车创建"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            onCompositionStart={() => { composingFolderName.current = true; }}
            onCompositionEnd={() => { composingFolderName.current = false; }}
            onBlur={submitCreate}
            onKeyDown={(e) => {
              if (composingFolderName.current || e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return;
              if (e.key === 'Enter') submitCreate();
              if (e.key === 'Escape') {
                setCreating(false);
                setDraftName('');
              }
            }}
          />
        )}
      </div>
      <div className={s.foot}>
        <CloudAccount />
      </div>
      <Modal
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && !deleting && setDeleteTarget(null)}
        title={deleteTarget?.kind === 'folder'
          ? '删除分组？'
          : deleteTarget?.projectType === 'discipline-map'
            ? '删除学习主题？'
            : '删除学习专题？'}
        description={deleteTarget?.kind === 'folder'
          ? '分组及组内路线、专题、附件、学习记录和会话将永久删除。'
          : '这个操作会永久删除项目内的附件、本地文件、学习记录和会话。'}
        size="sm"
        footer={(
          <>
            <Button variant="outline" disabled={deleting} onClick={() => setDeleteTarget(null)}>取消</Button>
            <Button variant="danger" loading={deleting} onClick={confirmDelete}>
              确认删除
            </Button>
          </>
        )}
      >
        <div className={s.deleteSummary}>
          <Icon name="x" size={18} />
          <div>
            <strong>{deleteTarget?.title}</strong>
            <span>删除后不能恢复</span>
          </div>
        </div>
        {deleteTarget?.kind === 'folder' && deleteTarget.projectSlugs.length > 0 && (
          <ul className={s.deleteProjects} aria-label="将删除的项目">
            {deleteTarget.projectSlugs.map(slug => <li key={slug}>{projectBySlug.get(slug)?.title ?? slug}</li>)}
          </ul>
        )}
        {(deleteTarget?.kind === 'folder' ? deleteFolder.error : deleteProject.error) && (
          <p className={s.saveError} role="alert">
            {(deleteTarget?.kind === 'folder' ? deleteFolder.error : deleteProject.error)?.message}
          </p>
        )}
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
        <span className={s.projectType}>{projectType === 'discipline-map' ? '主题' : '课堂'}</span>
      </Link>
      <div className={s.menuWrap}>
        <button
          ref={btnRef}
          type="button"
          className={`${s.menuBtn} ${menuOpen ? s.menuBtnOpen : ''}`}
          title="学习内容操作"
          aria-label={`学习内容操作：${title}`}
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
                  <div className={s.menuLabel}>移到分组</div>
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
                {projectType === 'discipline-map' ? '删除学习主题' : '删除学习专题'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
