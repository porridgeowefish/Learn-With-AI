生成学科总览：4–8个大章，每章2–6个可独立学习的主题。明确先修、并行、交叉与进阶关系；主题有唯一主归属，核心概念只由一个主题拥有，其余主题复用，兄弟主题不互为超集。

overviewMarkdown 使用 H1 学科名，H2 依次为“这门学科研究什么”“学科边界”“主要研究领域与知识架构”“领域之间如何关联”“常用研究方法与证据”“典型应用”。知识架构下 H3 为大章，H4 仅用于唯一主题。每个主题下用一两句话说明学习对象和核心问题。先修关系和讲解顺序写入主题字段。

只返回 JSON：{"overviewMarkdown":"正文","topics":{"schemaVersion":1,"topics":[...]}}。
每个主题包含 id（唯一稳定 kebab-case）、title、chapterTitle、goal、teachingOutline、inScope、outOfScope、prerequisites、ownedConcepts、reusedConcepts、priority。teachingOutline 是讲解顺序字符串；范围和概念字段为字符串数组。主题数量、顺序、标题和章归属须与 H4 一致，不添加路径或版本时间元数据。
priority 必填：I 核心必学或必要前置，II 中等深度学习，III 深入选学；按教学依赖判定，不平均分配。title 和 H4 不含评级或颜色。

general 模式按学科知识组织。sources 模式依据 sourceTexts 中的附件原文组织学科路线。partial 表示材料节选或提取不完整。在概览说明资料覆盖范围和使用的补充知识。可归纳章节，注明章节来自内容归纳。
