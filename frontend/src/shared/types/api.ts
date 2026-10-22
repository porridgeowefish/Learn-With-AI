// API DTOs — request/response shapes for the Go backend.
// Distinct from domain types when the wire format differs from the canonical
// in-memory shape (e.g. list endpoints wrap arrays in {<name>: [...]}).

import type {
  HealthResponse,
  ProjectMeta,
  ProjectType,
  ProjectState,
} from './domain';

// Responses
export interface ProjectsListResponse {
  projects: ProjectMeta[];
}

export interface ProjectResponse {
  project: ProjectState;
}

export interface DeleteProjectResponse {
  deleted: true;
  projectId: string;
}

// Requests
export interface CreateProjectRequest {
	 systemPrompt?: string;
  title: string;
  projectType: ProjectType;
  slug?: string;
  why?: string;
  current?: string;
  target?: string;
  standard?: string;
  scopeSource?: LearningScopeSource;
}

export interface LearningScopeSource {
  type: 'discipline-map';
  mapSlug: string;
  topicId: string;
  mapVersionId?: string;
}

export interface ProjectTypeAdviceRequest {
  title?: string;
  why?: string;
  messages: Array<{
    role: 'user' | 'assistant';
    content: string;
  }>;
}

export interface ProjectTypeAdviceResponse {
  reply: string;
  recommendation?: ProjectType;
  reason?: string;
  tradeoff?: string;
  confidence: 'low' | 'medium' | 'high';
}

export interface DisciplineTopicScope {

  priority?: 'I' | 'II' | 'III';
  id: string;
  title: string;
  chapterTitle: string;
  goal: string;
  inScope: string[];
  outOfScope: string[];
  prerequisites: string[];
  ownedConcepts: string[];
  reusedConcepts: string[];
}

export interface DisciplineTopicsResponse {
  schemaVersion: 1;
  topics: DisciplineTopicScope[];
  updatedAt: string;
}

export type DisciplineLearningTaskStatus = 'planned' | 'in-progress' | 'completed';

export interface DisciplineLearningTask {
  id: string;
  topicId?: string;
  topicTitle: string;
  status: DisciplineLearningTaskStatus;
  addedAt: string;
  startedAt?: string;
  completedAt?: string;
}

export interface DisciplineLearningPlanResponse {
  schemaVersion: 1;
  items: DisciplineLearningTask[];
  updatedAt: string;
}

// Re-export the health shape for convenience — it doubles as a DTO.
export type { HealthResponse };
