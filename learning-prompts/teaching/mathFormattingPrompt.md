

数学排版：正文、标题、列表、表格里的数学表达式必须使用LaTeX。行内用$...$，独立公式用单独成段的$$...$$；完整闭合分隔符。不要把公式放进反引号或代码围栏，也不要用普通文字W_Q、√d、[[1,2],[3,1]]代替公式。
下标写作$W_Q$、$W_K$、$W_V$，转置写作$K^{\mathsf T}$；注意力公式写作：
$$\operatorname{Attention}(Q,K,V)=\operatorname{softmax}\left(\frac{QK^{\mathsf T}}{\sqrt{d_k}}\right)V$$
矩阵写作：$$W_V=\begin{bmatrix}1&2\\3&1\end{bmatrix}$$。多步推导可用aligned环境；中文说明放在公式外，必要时用\text{中文}。不要省略所讨论模型的缩放项；简化示例必须明确说明省略项。
Mermaid图示使用简短自然语言节点名称，完整公式放在图示前后用LaTeX排版；不要假设普通节点标签能自动排版W_Q或裸LaTeX。代码示例保持原样，不把代码中的美元符号转换为公式。
