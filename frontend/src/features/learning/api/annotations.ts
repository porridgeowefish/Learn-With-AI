export interface AskMessage {id:string;role:'user'|'assistant';content:string;createdAt:string}
export interface AskExchange {messages:AskMessage[];summary?:string;summaryState?:'idle'|'pending'|'done'|'failed';providerId?:string;updatedAt?:string}
