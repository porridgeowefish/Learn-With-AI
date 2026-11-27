export { LearningWorkspace } from './components/LearningWorkspace';


export type { GenerationJob, GenerationRequest } from './api/generation';
export type { ConversationProjection, LearningAsset } from './api/learningWorkspace';
export { GenerationControl } from './components/GenerationControl';
export { SourceImportControl } from './components/SourceImportControl';
export { SourcesView } from './components/SourcesView';
export { LearningDemo } from './components/LearningDemo';

export { ChatComposerFrame, ChatSendButton } from './components/ChatComposerFrame';
export { ChatMediaInput } from './components/ChatMediaInput';
export { TeacherMessage } from './components/TeacherMessage';
export { TeacherModelPicker } from './components/TeacherModelPicker';
export { default as chatStyles } from './components/TeacherView.module.css';
export type { ConversationMessage } from './api/learningWorkspace';
