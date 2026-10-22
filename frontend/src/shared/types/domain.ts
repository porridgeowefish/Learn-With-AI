export type ProjectType = 'discipline-map' | 'system-learning';
export interface ProjectState {id:string;title:string;slug:string;projectType:ProjectType;status:string;createdAt:string;updatedAt:string;layoutVersion?:number}
export interface ProjectMeta {id:string;slug:string;title:string;projectType:ProjectType;overviewAvailable:boolean;layoutStatus?:'current'|'legacy-format-unsupported'|'project-layout-invalid'}
export interface HealthResponse {ok:boolean;workspace:string;stats:{projects:number}}
