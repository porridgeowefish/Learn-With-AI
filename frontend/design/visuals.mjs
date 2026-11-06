// Synthetic preview only: does not fetch Commons, PubChem, or a model.
export const previewFigureAnswer = '这是一张开放图示的展示样例，用来说明部件的相对位置。实际教学会保留检索结果的作者、许可和来源。\n\n```learning-visual\n' + JSON.stringify({provider:'commons', asset:'a/ab/Preview_gear.svg',title:'齿轮结构（设计预览示意）',author:'LLL 设计预览',license:'CC0',licenseUrl:'https://creativecommons.org/publicdomain/zero/1.0/',sourceUrl:'https://commons.wikimedia.org/wiki/Category:Gears'})+'\n```';

const manufacturing = [
  ['wafer', '前道：晶圆与重复建层', '晶圆上的芯片并行加工；薄膜、光刻、刻蚀等步骤会重复，不是每台设备只经过一次。', '<circle cx="230" cy="115" r="83"/><path d="M175 52v126m36-143v160m36-160v160m36-143v126M151 90h158m-164 36h170m-145 36h120"/>'],
  ['lithography', '前道：光刻中的图形转移', '对照掩模、光学系统和晶圆，观察图形如何转移到光刻胶层。', '<path d="M140 40h180M160 66h140M195 96h70m-100 110h130M160 66l50 115m90-115-50 115"/><ellipse cx="230" cy="125" rx="57" ry="13"/>'],
  ['package', '后道：封装与互连', '切割后的裸片与基板连接；此图只表达封装互连的结构关系。', '<path d="M130 190h220v20H130zM190 135h100v35H190zM160 190q0-100 40-55m80 0q40-45 40 55"/>'],
  ['transport', '厂内：载具与搬运', '厂内物流连接不同工艺设备，与芯片内部结构和工艺循环是不同层面的关系。', '<path d="M90 55h300M145 55v40h70V55m-65 40v55h60V95M80 205h320M110 170h70v35h-70zM290 170h70v35h-70z"/>'],
];
const previewMetadata = (asset, title, sourceUrl) => ({provider:'commons', asset, title:title+'（设计预览示意）',author:'LLL 设计预览',license:'CC0',licenseUrl:'https://creativecommons.org/publicdomain/zero/1.0/',sourceUrl});
export const previewManufacturingAnswer = '先区分前道晶圆制造、后道封装测试，以及厂内物流。这些合成预览图只演示分阶段讲解和来源展示，不代表某一工厂的完整产线。\n\n```mermaid\nflowchart LR\n A[前道晶圆制造] --> B[切割与封装]\n B --> C[测试]\n D[厂内搬运] -.连接设备.-> A\n```\n\n'+manufacturing.map(([id,title,caption])=>'## '+title+'\n\n'+caption+'\n\n```learning-visual\n'+JSON.stringify(previewMetadata('a/ab/Preview_'+id+'.svg',title,'https://commons.wikimedia.org/wiki/Category:Semiconductor_manufacturing'))+'\n```').join('\n\n')+'\n\n本次预览没有提供测试设备的外部结构图。正式检索会列出未找到合适图示的环节，可继续按具体工序展开。';

export function visuals({path, method, url}) {
  const asset = url.searchParams.get('asset');
  const stage = manufacturing.find(([id])=>asset==='a/ab/Preview_'+id+'.svg');
  if (path === '/api/learning/visuals/metadata' && method === 'GET') return Response.json(stage ? previewMetadata(asset,stage[1],'https://commons.wikimedia.org/wiki/Category:Semiconductor_manufacturing') : {provider:url.searchParams.get('provider'),asset,title:'齿轮结构（设计预览示意）',author:'LLL 设计预览',license:'CC0',licenseUrl:'https://creativecommons.org/publicdomain/zero/1.0/',sourceUrl:'https://commons.wikimedia.org/wiki/Category:Gears'});
  if (path !== '/api/learning/visuals/image' || method !== 'GET') return;
  if (stage) return new Response('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 480 260"><rect width="480" height="260" fill="white"/><g fill="none" stroke="#4b736f" stroke-width="3">'+stage[3]+'</g><text x="14" y="246" font-size="12" fill="#364f4c">设计预览 · 合成示意图</text></svg>',{headers:{'Content-Type':'image/svg+xml','Content-Security-Policy':"default-src 'none'; sandbox"}});
  return new Response('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 480 260"><rect width="480" height="260" fill="white"/><g fill="none" stroke="#4b736f" stroke-width="12"><circle cx="240" cy="115" r="72" stroke-dasharray="17 9"/><circle cx="240" cy="115" r="52"/><circle cx="240" cy="115" r="16"/></g><path d="M310 90h70m-140 42v65" stroke="#af7545" stroke-width="2"/><g font-size="17" fill="#364f4c"><text x="384" y="96">齿面</text><text x="220" y="220">轴孔</text><text x="14" y="246" font-size="12">设计预览 · 合成示意图</text></g></svg>',{headers:{'Content-Type':'image/svg+xml','Content-Security-Policy':"default-src 'none'; sandbox"}});
}
