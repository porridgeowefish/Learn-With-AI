// Package bootstrap assembles current services, recovery and event connections.
package bootstrap

import (
	"context"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/activity"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/preferences"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/modules/treasures"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/transport/httpserver"
	"time"
)

type App struct{ Server *httpserver.Server }

func Build() *App {
	if err := preferences.BackupExisting(); err != nil {
		println("preferences backup:", err.Error())
	}
	teacher.ReconcileAllInterruptedResponses()
	broadcaster := httpx.NewBroadcaster()
	sources.ConfigureStructureEvents(func(slug string, job sources.StructureJob) {
		broadcaster.Emit("source-structure-job-updated", map[string]any{"projectSlug": slug, "sourceId": job.SourceID, "jobId": job.ID, "status": job.Status, "coverage": job.Coverage})
	})
	rewards := treasures.New(paths.WORKSPACE)
	teaching := teacher.New(nil)
	teaching.OnSourceReading = func(event map[string]any) { broadcaster.Emit("source-reading", event) }
	teaching.OnSourceUpdated = func(slug string, source sources.Source) {
		broadcaster.Emit("source-updated", map[string]any{"projectSlug": slug, "source": source})
	}
	processor := sources.NewProcessor(func(slug string, source sources.Source) {
		broadcaster.Emit("source-updated", map[string]any{"projectSlug": slug, "source": source})
	})
	processor.Start(context.Background())
	jobs := generation.New(nil)
	jobs.OnUpdated = func(slug string, job generation.Job) {
		broadcaster.Emit("generation-job-updated", map[string]any{"projectSlug": slug, "jobId": job.ID, "status": job.Status, "phase": job.Phase, "progress": job.Progress})
	}
	if err := jobs.Start(context.Background()); err != nil {
		println("generation startup:", err.Error())
	}
	recordActivity := func(slug string, e activity.ProgressEvent) {
		_, added, err := activity.RecordActivity(slug, e)
		if err == nil && added {
			broadcaster.Emit("learning-activity-updated", map[string]any{"projectSlug": slug})
		}
	}
	lifecycle := teacher.NewLifecycle(teaching)
	lifecycle.OnQueueUpdated = func(slug string) { broadcaster.Emit("conversation-queue-updated", map[string]any{"projectSlug": slug}) }
	tools := teacher.NewLearningTools()
	tools.OnActivity = recordActivity
	tools.OnFeynmanCompleted = func(slug, sessionID, messageID, content string) {
		grantTreasure(rewards, broadcaster, slug, "feynman", sessionID, messageID, content)
	}
	tools.OnAnnotationUpdated = func(slug, action, id string) {
		broadcaster.Emit("annotation-updated", map[string]any{"projectSlug": slug, "action": action, "id": id})
	}
	server := httpserver.New(httpserver.Dependencies{Lifecycle: lifecycle, Tools: tools, Teacher: teaching, Generation: jobs, SourceProcessor: processor, Broadcaster: broadcaster, Cache: projects.NewIndex(), Treasures: rewards})
	notes := teacher.NewNoteService(nil)
	notes.OnInitializationFailure = func(slug string, err error) { println("notes initialization:", slug, err.Error()) }
	notes.OnUpdated = func(slug, jobID, status, noteID string) {
		broadcaster.Emit("teacher-note-updated", map[string]any{"projectSlug": slug, "jobId": jobID, "status": status, "noteId": noteID})
	}
	if err := notes.Start(context.Background()); err != nil {
		println("notes startup:", err.Error())
	}
	server.AttachNotes(notes)
	teaching.OnCompleted = func(slug, responseID string) {
		if store, err := teacher.NewConversation(slug); err == nil {
			if info, found, err := store.ResponseByID(responseID); err == nil && found {
				if messages, err := store.AllMessages(); err == nil {
					for _, message := range messages {
						if message.ID == info.TriggeringLearnerMessageID && message.Role == "learner" {
							content := ""
							for _, block := range message.Blocks {
								if block.Source != "" {
									content += block.Source + "\n"
								} else if block.ArtifactRef != "" {
									content += block.ArtifactRef + "\n"
								}
							}
							grantTreasure(rewards, broadcaster, slug, "teacher", message.ID, message.ID, content)
						}
					}
				}
			}
		}
		recordActivity(slug, activity.ProgressEvent{ID: "teacher-response:" + responseID, SourceType: "teacher-response", SourceID: responseID, ActivityDelta: 1, Title: "完成教师对话"})
		if err := notes.ObserveCompleted(slug); err != nil {
			println("teacher notes:", slug, err.Error())
		}
	}
	return &App{Server: server}
}

func grantTreasure(store *treasures.Store, b *httpx.Broadcaster, slug, kind, sourceID, messageID, content string) {
	project, err := projects.ReadProjectState(slug)
	if err != nil {
		return
	}
	reward, err := store.Complete(treasures.Completion{Key: project.ID + ":" + project.CreatedAt.Format(time.RFC3339Nano) + ":" + kind + ":" + messageID, Kind: kind, ProjectID: project.ID, ProjectCreatedAt: project.CreatedAt.Format(time.RFC3339Nano), ProjectSlug: slug, ProjectTitle: project.Title, SourceID: sourceID, MessageID: messageID, Text: content})
	if err != nil {
		println("treasure reward:", err.Error())
		return
	}
	if reward != nil {
		b.Emit("treasure-awarded", reward)
	}
}
