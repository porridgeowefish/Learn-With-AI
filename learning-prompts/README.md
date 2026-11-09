# 提示词维护入口

文本集中在这里；对话、偏好原文、OCR 结果和知识摘要是运行数据，不存放在此。

| 调整内容 | 文件 |
| --- | --- |
| 教师共同规则 | teaching/teacher.md |
| 回复长短 | teaching/response-length.json：short、medium、long |
| 公式与图示格式 | teaching/formatting.md |
| 交互数学接口 | teaching/interactive.md |
| 开场、结束、工具状态 | teaching/runtime.json |
| 偏好、范围、资料包裹 | teaching/context.json |
| 自由、书籍、历史、概念教法 | modes/ 下对应 Markdown |
| 费曼学习 | feynman/base.md；短模板在 feynman/context.json |
| 复习、练习、学科地图 | review.md、practice.md、discipline-map.md |
| 生成任务共同边界 | common.md |
| 工具使用及描述 | tools/sources.json、web-search.json、visual.json |
| 教学结束归纳工具 | tools/summarize-taught-concepts.md |
| 摘要、笔记、OCR、意图识别等独立任务 | auxiliary/ 下对应 Markdown；批注短模板在 annotation.json |
| 场景组装 | profiles/ |

Markdown 用于完整任务提示。JSON 的 prompts 对象集中保存同类短文本，每个键对应一个独立条目。例如 teaching/response-length/short 只读取 response-length.json 的 short，不注入其它档位。每条独立替换变量；其它条目的变量无需提供。

调整流程：
1. 修改文本；保留双花括号变量名、JSON 输出字段和工具标识。
2. 调整加载组合时修改 profiles 中的资源引用、条件和工具清单。未命中的上下文来源不读取。
3. 运行 powershell -File scripts/update-prompt-manifest.ps1，更新 UTF-8、LF 和资源 hash。
4. 重启应用，保持模型、输入和偏好一致进行试学。

复习只组合 common + review，练习只组合 common + practice，教法各由主提示负责，没有额外方法层。
内置学习模式只维护 modes/ 下当前四份文本，新项目引用当前资源。已有 v1 项目的原文快照、进行中的请求和已封存任务不被覆盖。
资源 hash 校验失败会报错，不回退到另一份提示。新增数据来源或工具能力仍需实现注册；更改文本和已有资源组合不需修改业务流程。
