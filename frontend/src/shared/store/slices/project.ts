import {create} from 'zustand';
import {persist} from 'zustand/middleware';
interface ProjectSelection {selectedProjectId:string|null;selectProject:(id:string|null)=>void}
export const useProjectStore=create<ProjectSelection>()(persist((set)=>({selectedProjectId:null,selectProject:(id)=>set({selectedProjectId:id})}),{name:'lll.project',partialize:(state)=>({selectedProjectId:state.selectedProjectId})}));
