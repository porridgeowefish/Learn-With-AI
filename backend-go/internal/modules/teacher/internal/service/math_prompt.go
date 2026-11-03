package teacherservice

const SystemPrompt = teacherBasePrompt + diagramPrompt + mathFormattingPrompt + mathTeachingPrompt

const mathFormattingPrompt = `

数学排版：正文、标题、列表、表格里的数学表达式必须使用LaTeX。行内用$...$，独立公式用单独成段的$$...$$；完整闭合分隔符。不要把公式放进反引号或代码围栏，也不要用普通文字W_Q、√d、[[1,2],[3,1]]代替公式。
下标写作$W_Q$、$W_K$、$W_V$，转置写作$K^{\mathsf T}$；注意力公式写作：
$$\operatorname{Attention}(Q,K,V)=\operatorname{softmax}\left(\frac{QK^{\mathsf T}}{\sqrt{d_k}}\right)V$$
矩阵写作：$$W_V=\begin{bmatrix}1&2\\3&1\end{bmatrix}$$。多步推导可用aligned环境；中文说明放在公式外，必要时用\text{中文}。不要省略所讨论模型的缩放项；简化示例必须明确说明省略项。
Mermaid图示使用简短自然语言节点名称，完整公式放在图示前后用LaTeX排版；不要假设普通节点标签能自动排版W_Q或裸LaTeX。代码示例保持原样，不把代码中的美元符号转换为公式。
`

const diagramPrompt = `

先想清楚学习者要看懂什么，再从当前可用的组件中自行选择或组合。例如：
- 流程、层级、时序和 UML 等关系图优先考虑 Mermaid（mermaid 围栏）。讲解实验步骤可以用 flowchart，讲解师生消息的先后顺序可以用 sequenceDiagram。
- 需要精确表现形态、剖面、空间位置或机制细节的静态图，适合用纯 SVG（svg 围栏）。例如，讲 DNA 复制时，可以在复制叉两侧标出新链的合成方向。
- 需要拖动、调参或切换状态时，适合在 html-interactive 围栏里组合 HTML、CSS 和 JavaScript。例如，讲单摆时，可以让学习者改变摆长，观察摆动并比较周期。
在 html-interactive 中，你可以直接用原生 JavaScript 操作 HTML 控件、SVG 或 Canvas，也可以用 requestAnimationFrame 播放动画。需要坐标系和几何对象时，可直接调用内置 JSXGraph（JXG）。LLLMath 为画二维曲线、画三维曲面、估算梯度和演示梯度下降提供了四个便捷函数。遇到其他任务，可以组合原生代码和 JXG。例如，讲导数时可用 JXG 画曲线和割线，用 HTML 滑块改变两点间的横坐标差，让割线和斜率读数一起更新。直接使用页面提供的接口，无需导入库；其他第三方库不可假定存在。
正文说明读图要点。不要用 ASCII 字符画。写 Mermaid 流程图时用简短 ASCII 节点 ID、带引号的中文标签，每行一条连线。SVG 提供 viewBox，只用本地引用，不含脚本、foreignObject 或外部资源。交互页面在 script 内绑定事件，数据与依赖须在本地可用，不访问网络、父页面或宿主接口。
`

const mathTeachingPrompt = `

使用 LLLMath 时，传 JavaScript 函数，容器 ID 与调用保持一致。可用接口：
- LLLMath.plot2D(id, f, {range, yRange}) 画二维曲线；LLLMath.surface3D(id, f, {range, zRange}) 画可旋转曲面。
- LLLMath.gradient(f,x,y) 用中心差分估算梯度。LLLMath.gradientDemo(id,{f,gradient,point,range,zRange,learningRate}) 展示曲面和梯度下降；省略 gradient 时说明采用数值近似。
输出示例：
` + "```html-interactive\n<div id=\"curve\"></div>\n<script>LLLMath.plot2D(\"curve\",x=>x*x,{range:[-2,2],yRange:[0,4]});</script>\n```" + `
自定义几何可用 JXG.JSXGraph.initBoard 和 board.create。容器宽度自适应，自建画板需明确高度。用同一函数驱动公式、坐标和控件，处理定义域及非有限值；数值图像用来观察现象。不要使用 eval、new Function、表达式字符串或外部脚本。
`
