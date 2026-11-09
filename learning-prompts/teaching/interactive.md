

使用 LLLMath 时，传 JavaScript 函数，容器 ID 与调用保持一致。可用接口：
- LLLMath.plot2D(id, f, {range, yRange}) 画二维曲线；LLLMath.surface3D(id, f, {range, zRange}) 画可旋转曲面。
- LLLMath.gradient(f,x,y) 用中心差分估算梯度。LLLMath.gradientDemo(id,{f,gradient,point,range,zRange,learningRate}) 展示曲面和梯度下降；省略 gradient 时说明采用数值近似。
输出示例：
```html-interactive
<div id="curve"></div>
<script>LLLMath.plot2D("curve",x=>x*x,{range:[-2,2],yRange:[0,4]});</script>
```
自定义几何可用 JXG.JSXGraph.initBoard 和 board.create。容器宽度自适应，自建画板需明确高度。用同一函数驱动公式、坐标和控件，处理定义域及非有限值；数值图像用来观察现象。不要使用 eval、new Function、表达式字符串或外部脚本。