// Validate the browser preview format before any records enter IndexedDB.
const string = value => typeof value === 'string';
const number = Number.isFinite;
const bool = value => typeof value === 'boolean';
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const optional = check => value => value === undefined || check(value);
const list = check => value => Array.isArray(value) && value.every(check);
const dict = check => value => object(value) && Object.values(value).every(check);
const record = fields => value => object(value) && Object.entries(fields).every(([key,check])=>check(value[key]));
const unsafe = key => key === 'prototype' || Object.hasOwn(Object.prototype,key);
const slug = value => string(value) && value.trim() && !unsafe(value) && !/[\\/\x00-\x1f]/.test(value);
const date = value => string(value) && Number.isFinite(Date.parse(value));
const strings = list(string);
const textMessage = record({id:string,role:string,content:string,status:string,createdAt:date});
const block = record({id:string,type:string,source:optional(string),name:optional(string),artifactRef:optional(string)});
const project = record({
  id:slug,slug,title:string,projectType:v=>['discipline-map','system-learning'].includes(v),
  status:string,overviewAvailable:bool,createdAt:date,updatedAt:date,systemPrompt:optional(string),learningMode:optional(v=>['general','history','book','concept'].includes(v)),
  scopeSource:optional(record({mapSlug:slug,topicId:string})),
});
const folder = record({id:slug,name:string,slugOrder:list(slug),mapProjectSlug:optional(slug)});
const activity = record({id:string,sourceId:string,sourceType:string,title:string,detail:optional(string),
  createdAt:date,projectSlug:slug,projectTitle:string,activityDelta:number,delta:number});
const reward = record({id:slug,itemId:slug,kind:string,projectSlug:slug,projectId:string,projectTitle:string,
  sourceId:string,messageId:string,createdAt:date,isNew:bool,seen:bool,welcome:optional(bool)});
const asset = record({content:string,contentKind:string,meta:record({assetId:string,key:string,title:string,
  currentVersionId:string,editRevision:number,updatedAt:date})});
const source = record({
  source:record({sourceId:string,displayName:string,currentRevisionId:string,status:string,
    extractedBytes:optional(number),coverage:optional(record({complete:bool}))}),
  revision:record({sourceId:string,revisionId:string,original:object,derivedFiles:list(record({key:string,path:string}))}),
  parsedText:optional(string),jobs:list(record({id:string,status:string})),
});
const learning = record({
  assets:record({review:asset,practice:asset}), mapVersion:string,systemPrompt:string,learningMode:optional(v=>['general','history','book','concept'].includes(v)),
  topics:list(record({id:string,title:string,chapterTitle:string,goal:string,inScope:strings,outOfScope:strings,
    prerequisites:strings,ownedConcepts:strings,reusedConcepts:strings})),
  plan:record({items:list(record({id:string,topicTitle:string,status:string,addedAt:date})),updatedAt:date}),
  messages:list(record({id:string,role:string,status:string,createdAt:date,blocks:list(block)})),
  sources:list(source),media:object,queue:list(record({queueId:string,content:string,queuedAt:date,
    mediaBlocks:optional(list(block)),attachmentRefs:optional(strings),conversationAttachmentIds:optional(strings)})),
  jobs:list(record({id:string,kind:string,status:string,phase:string,updatedAt:date,progress:object})),
  annotations:list(record({annotationId:string,assetId:string,assetVersionId:string,quoteSnapshot:string,
    anchors:record({start:number,end:number}),note:optional(string),ask:optional(record({messages:list(textMessage),summary:optional(string)}))})),
  sessions:list(record({sessionId:string,messages:list(textMessage),createdAt:date,updatedAt:date})),
  rounds:list(record({versionId:string,content:string,createdAt:date})),
  results:dict(record({versionId:string,answers:dict(strings),items:dict(record({correct:bool,expected:strings})),score:number,total:number,submittedAt:date})),
  notes:record({entries:list(record({id:string,content:string,fromTurn:number,throughTurn:number,createdAt:date})),
    jobs:list(record({id:string,status:string,fromTurn:number,throughTurn:number,retryable:bool}))}),
});
const snapshot = record({version:v=>v===1,projects:list(project),folders:list(folder),activity:list(activity),
  rewards:list(reward),learning:dict(learning)});
export function parseSnapshot(text) {
  const data = JSON.parse(text,(key,value)=>{
    if (unsafe(key)) throw new Error('记录包包含无效字段。');
    return value;
  });
  if (!snapshot(data)) throw new Error('记录包格式无效。');
  const slugs = new Set(data.projects.map(p=>p.slug));
  if (slugs.size !== data.projects.length || Object.keys(data.learning).some(key=>!slugs.has(key))) {
    throw new Error('记录包中的项目标识不一致。');
  }
  return data;
}
