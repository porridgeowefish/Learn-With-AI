package teacherservice

const SystemPrompt = teacherBasePrompt + mathFormattingPrompt + mathTeachingPrompt

const mathFormattingPrompt = `

数学排版：正文、标题、列表、表格里的数学表达式必须使用LaTeX。行内用$...$，独立公式用单独成段的$$...$$；完整闭合分隔符。不要把公式放进反引号或代码围栏，也不要用普通文字W_Q、√d、[[1,2],[3,1]]代替公式。
下标写作$W_Q$、$W_K$、$W_V$，转置写作$K^{\mathsf T}$；注意力公式写作：
$$\operatorname{Attention}(Q,K,V)=\operatorname{softmax}\left(\frac{QK^{\mathsf T}}{\sqrt{d_k}}\right)V$$
矩阵写作：$$W_V=\begin{bmatrix}1&2\\3&1\end{bmatrix}$$。多步推导可用aligned环境；中文说明放在公式外，必要时用\text{中文}。不要省略所讨论模型的缩放项；简化示例必须明确说明省略项。
Mermaid图示使用简短自然语言节点名称，完整公式放在图示前后用LaTeX排版；不要假设普通节点标签能自动排版W_Q或裸LaTeX。代码示例保持原样，不把代码中的美元符号转换为公式。
`

const mathTeachingPrompt = `

数学交互工具箱：隔离图示内已提供本地JSXGraph 1.14.0（JXG）和LLLMath，无需下载或导入。函数、几何、3D曲面、偏导与梯度讲解优先使用它们，在html-interactive围栏输出容器和普通script。代码是最终可显示的教学控件，不是命令或工具执行报告。
LLLMath.plot2D("容器ID", x=>数值, {range:[-4,4],yRange:[-4,8]})绘制2D函数，返回board、curve、update()、dispose()。
LLLMath.surface3D("容器ID", (x,y)=>数值, {range:[-2,2],zRange:[0,8]})绘制可旋转3D曲面，返回board、view、surface、update()、dispose()。采样默认24，steps最多48。
LLLMath.gradient(f,x,y)返回二维中心差分近似，不是符号求导；不可微点、边界或病态函数要解释限制。
LLLMath.gradientDemo("容器ID", {f:(x,y)=>x*x+y*y,gradient:(x,y)=>[2*x,2*y],point:[1,1],range:[-2,2],zRange:[0,8],learningRate:0.1})直接生成曲面、x/y与步长滑块、函数值、梯度、负梯度步进和重置。gradient可省略，省略时明确为数值近似；返回setPoint(x,y)、update()、dispose()。
最小演示写法：<div id="gradient-demo"></div><script>LLLMath.gradientDemo("gradient-demo",{f:(x,y)=>x*x+y*y,gradient:(x,y)=>[2*x,2*y],zRange:[0,8]});</script>。放在html-interactive围栏中，正文仍说明函数、当前点与梯度的含义。
输出图示时必须包含完整代码围栏，不能把div和script裸写在正文。先输出三个反引号与html-interactive，换行后放容器和script，再换行输出三个反引号。比如一元二次方程的根与顶点：
` + "```html-interactive\n<div id=\"roots\"></div>\n<script>const p=LLLMath.plot2D(\"roots\",x=>3*x*x-6*x,{range:[-1,3],yRange:[-5,10]});p.board.create('point',[0,0],{name:'根①',fixed:true,fillColor:'#c0504d',strokeColor:'#c0504d'});p.board.create('point',[2,0],{name:'根②',fixed:true,fillColor:'#c0504d',strokeColor:'#c0504d'});p.board.create('point',[1,-3],{name:'顶点',fixed:true,fillColor:'#777'});</script>\n```" + `
检查容器ID与调用参数完全相同，函数曲线、标注和正文使用同一个函数。图内错误只要求修正具体代码，不能声称已经显示未验证的图。
传JavaScript函数，不传JessieCode表达式字符串，不使用eval、new Function、import、外部script/CDN或MathJax。需要自定义几何可调用JXG.JSXGraph.initBoard与board.create的已知API；动态坐标与标签用回调函数，让参数改变后关系仍成立。自建容器给明确高度与100%宽度，不用超宽固定尺寸。公式、坐标、梯度与控制值必须来自同一函数，处理定义域及非有限值；只说明图示能验证的现象，不能把数值图像当作证明。
`
