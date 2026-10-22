// Package projectservice coordinates project creation and deletion across modules.
package projectservice

import (
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

type Generation interface {
	DeleteProject(string) error
	ReviveProject(string)
}
type Service struct {
	Teaching   *teacher.Lifecycle
	Tools      *teacher.LearningTools
	Notes      *teacher.NoteService
	Generation Generation
}

func (s *Service) Create(slug, title string, in projects.ProjectInput) (*projects.ProjectState, error) {
	if err := projects.CreateProjectSkeletonWithInput(slug, title, "", in); err != nil {
		return nil, err
	}
	if projects.NormalizeProjectType(in.ProjectType) == projects.ProjectTypeSystemLearning {
		if _, err := teacher.NewConversation(slug); err != nil {
			return nil, err
		}
		if _, err := assets.New(slug); err != nil {
			return nil, err
		}
	}
	if s.Generation != nil {
		s.Generation.ReviveProject(slug)
	}
	if s.Teaching != nil {
		s.Teaching.ReviveProject(slug)
	}
	if s.Tools != nil {
		s.Tools.ReviveProject(slug)
	}
	if s.Notes != nil {
		s.Notes.ReviveProject(slug)
	}
	return projects.ReadProjectState(slug)
}
func (s *Service) Delete(slug string) error {
	remove := projects.DeleteProject
	if s.Generation != nil {
		remove = s.Generation.DeleteProject
	}
	if s.Notes != nil {
		next := remove
		remove = func(slug string) error { return s.Notes.DeleteProject(slug, next) }
	}
	if s.Tools != nil {
		next := remove
		remove = func(slug string) error { return s.Tools.DeleteProject(slug, next) }
	}
	if s.Teaching != nil {
		next := remove
		remove = func(slug string) error { return s.Teaching.DeleteProject(slug, next) }
	}
	if err := remove(slug); err != nil {
		return err
	}
	folders, err := projects.NewFolderStore()
	if err != nil {
		return err
	}
	_, err = folders.RemoveProject(slug)
	return err
}
