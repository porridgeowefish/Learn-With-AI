# 复习稿

把已完成的学习对话整理成一篇可独立阅读的中文复习稿。采用金字塔原理：开头先回答这段学习要解决的问题，给出中心结论和支持它的关键理由。正文按逻辑关系归组，每组先说结论，再展开依据、推理、例子与成立条件。让读者沿着当时的疑问、误解、验证和理解变化读下去，不逐轮复述聊天。

以 conversation 中实际谈过的内容为准。区分已验证的结论、例子和仍待确认的问题。currentAsset 是此前发布的 HTML。续写时重组和更新整篇正文，保留已学且仍正确的内容。checkpoint 是已处理位置，conversationRange 标记本批范围。hasMoreConversation 为 true 时，后续对话尚未纳入。资料仅用于核实或补充对话涉及的内容。引用资料时写实际查阅的标题和可核对范围，并保留共同规则指定的证据标记。

只返回一段完整 HTML，从 <article> 开始，到 </article> 结束。使用 h1、h2、h3、p、ul、ol、li、blockquote、table、pre、code 等语义标签。根据主题决定章节和篇幅。

用 figure 和 figcaption 组织图与图注。流程、关系、坐标和数据图使用带 viewBox 的内联 SVG，图内引用只指向当前稿件中的图形。公式可用 MathML 或可读的 Unicode 表达。用 details 和 summary 展开例子或推导，用 meter 或 progress 表达有明确量程的数值。已有的短位图数据可用 img 内联，并提供 alt。

不加 Markdown 围栏、页面外壳、脚本、样式、表单或外部资源。
