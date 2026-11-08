# 提示词维护入口

这里保存应用预设的提示文本和场景组合。修改文字在 Markdown 中进行；调整已有提示、资料来源的组合在 profiles 中进行。对话记录、学习者偏好原文、OCR 结果和知识摘要属于运行数据，不复制到本目录。

| 想调整什么 | 文件或目录 |
| --- | --- |
| 自由学习的教法 | modes/general.md |
| 历史、书籍、概念学习方式 | modes/history.md、book.md、concept.md |
| 教师共同规则、开场、数学和图示规则 | teaching/ |
| 教师每轮加载顺序、条件、资料绑定及工具名称 | profiles/teacher/general.json |
| 学科地图、复习稿、出题 | discipline-map.md、review.md、practice.md，及 common.md |
| 费曼讲解 | feynman/base.md |
| 结束意图识别 | auxiliary/tool-intent.md |
| 结束后的知识概括 | auxiliary/section-summary.md |
| 既有自动笔记、批注答疑和批注总结 | auxiliary/notes.md、annotation-ask.md、annotation-summary.md |
| 工具描述、使用说明 | tools/ |
| 资料图片文字提取、学习方式顾问 | auxiliary/ocr.md、project-advice.md |
| 既有学习背景提取 | auxiliary/memory-extraction.md |

一次常规调整：

1. 修改对应 Markdown。变量写成双花括号包围的英文变量名；变量名称须与调用契约一致，JSON 与数学公式保持原样。
2. 如需改变组合，编辑场景的 slots、contextBindings、when 或 tools。工具清单只选择已经实现的能力；删除工具名称会使其不能在该场景执行。未命中的资料绑定不会读取数据。
3. 在仓库根运行 `powershell -File scripts/update-prompt-manifest.ps1`，重新生成资源 hash 与变量声明。
4. 重启本地应用，使用固定的测试问题复测。需要比较模型行为时保持提供方、模型、偏好和输入一致，分别修改教学提示、结束识别提示或知识概括提示。

资源校验失败会报错，不回退到另一份源码提示。正在进行的模型请求和已经封存的生成任务保持原输入。新创建项目的学习方式保存 v2 资源引用；旧项目的 v1 原文快照继续保留，不按名称猜测并覆盖用户已有内容。调优旧项目时应留意其仍使用旧快照，可用新测试项目验证当前内置模式。

场景支持有限条件表达式与具名数据源，不运行脚本。添加新提示文本或重组已有数据源不需要修改业务流程；增加一种系统尚未提供的数据来源或新工具实现，仍需实现并注册相应接口。

开发验证使用 Go 测试中的假 Gateway 检查实际请求和工具调用。教师运行事件保留 tool-call-requested、tool-result-recorded、tool-intent-evaluated；知识摘要另有 teaching-end-confirmed 及摘要状态查询。模型收到提示并不等于遵守提示，教学表达和摘要质量需结合实际输出评价。
