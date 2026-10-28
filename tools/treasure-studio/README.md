# 潮汐藏室 · Tidal Cabinet

独立的学习宝物素材工作室。包含50件GLB宝物、六套实时海滩预设、动态海水和素材检查网页。

## 使用

在本目录执行：

```sh
npm install
npm run dev
```

打开 http://127.0.0.1:5198 。素材已生成，无需重新导出。构建：npm run build；模型检查：npm test。

重新制作的顺序：npm run assets → Blender 后台执行 scripts/refine-assets.py → node scripts/export-gem-cuts.mjs → Blender 后台执行 scripts/refine-assets.py -- --stones → Blender 后台执行 scripts/sync-gem-library.py。最后通过 src/thumbnails.js 的 renderThumbnails 重新生成实际模型缩略图。单独执行 assets 会重置清单为基础版本。

## 文件

- public/assets/manifest.json 中的 url 指向当前版本：refined/ 为 Blender 纹理细化模型，jewelry/ 为重新制作的宝石切面。根目录 GLB 是基础版本。
- public/assets/manifest.json：品种、收藏级别、文件大小、面数和SHA-256。
- public/assets/beaches.json：六套海滩预设。
- src/assets.js：可复用的程序化模型制作代码。
- src/gem-optics.js：透明宝石的 BVH 内部反射、折射、色散及本地珠宝摄影灯光。
- sources/gem-library.blend：脚本在本机生成的25种宝石可编辑 Blender 模型库。sources/ 中的工程和烘焙纹理保持本地保存；GitHub 发布可运行的 GLB 素材与重建脚本。
- src/plate-environment.js：原图背景与 Blender Ocean 数据驱动的水面合成；src/ocean.js 是未采用的早期原型。
- public/references/：已选背景原图，作为背景和选择缩略图使用。

网页支持贝壳/宝石切换、五级筛选、搜索、模型旋转缩放、GLB下载、动态海滩、材质检查、海浪暂停、画质和浪高调整。沉浸模式按Esc退出。键盘聚焦3D画布后用方向键旋转，+/-缩放。

## 素材边界

模型依据美术图进行程序化重建，并非图像扫描或手工雕刻复原。透明切面宝石在网页内使用最多8次内部光线追踪，GLB保留通用物理材质；下载后由其他软件渲染，外观不保证与网页相同。宝石灯光为程序化摄影棚环境，不追踪海滩背景照明。

海滩是保留原图配色的2.5D动态背景，水面由Blender Ocean频谱数据驱动局部扰动；不是完整三维海岸、真实破浪流体或视频。没有调用Runway或其他付费服务。

此工作室不读写LLL学习数据，也不实现随机奖励系统。测试、截图和检查结论分别见本地Harness迭代和根trash目录。
