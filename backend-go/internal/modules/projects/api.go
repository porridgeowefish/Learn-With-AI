// Package projects is the public boundary for project workspaces, folders, and
// the runtime project index. Filesystem details stay private to this module.
package projects

import (
	"github.com/xmz14/lll/backend-go/internal/modules/projects/internal/folderstore"
	"github.com/xmz14/lll/backend-go/internal/modules/projects/internal/projectindex"
	"github.com/xmz14/lll/backend-go/internal/modules/projects/internal/workspace"
)

type ProjectType = workspace.ProjectType
type ProjectState = workspace.ProjectState
type ProjectMeta = workspace.ProjectMeta
type ProjectInput = workspace.ProjectInput
type SlugConflictError = workspace.SlugConflictError

const (
	ProjectTypeDisciplineMap  = workspace.ProjectTypeDisciplineMap
	ProjectTypeSystemLearning = workspace.ProjectTypeSystemLearning
)

var NormalizeProjectType = workspace.NormalizeProjectType
var ValidateProjectType = workspace.ValidateProjectType
var ValidateSlug = workspace.ValidateSlug
var ProjectExists = workspace.ProjectExists
var DeleteProject = workspace.DeleteProject
var CreateProjectSkeleton = workspace.CreateProjectSkeleton
var CreateProjectSkeletonWithInput = workspace.CreateProjectSkeletonWithInput
var ReadProjectState = workspace.ReadProjectState
var WriteProjectState = workspace.WriteProjectState
var IndexAll = workspace.IndexAll
var IsSlugConflict = workspace.IsSlugConflict
var FindProjectBySlug = workspace.FindProjectBySlug
var EnsureProjectsRoot = workspace.EnsureProjectsRoot
var ProjectRootForSlug = workspace.ProjectRootForSlug
var ProjectsRootForTest = workspace.ProjectsRootForTest
var SetProjectsRootForTest = workspace.SetProjectsRootForTest
var Slugify = workspace.Slugify

type Folder = folderstore.Folder
type MapFolderSpec = folderstore.MapFolderSpec
type Layout = folderstore.Layout
type FolderLayout = folderstore.Layout
type FolderStore = folderstore.Store

func NewFolderStore() (*FolderStore, error) { return folderstore.New() }

type Index = projectindex.Cache

func NewIndex() *Index { return projectindex.New() }

var ErrLegacyFormat = workspace.ErrLegacyFormat
var ErrInvalidLayout = workspace.ErrInvalidLayout
