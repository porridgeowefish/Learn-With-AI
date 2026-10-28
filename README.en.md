<div align="center">

<img src="frontend/public/app-icon.png" width="80" alt="Learn-With-AI app icon" />

# Learn-With-AI — Local-first AI Learning App

[简体中文](./README.md) · English

**Learn-With-AI is an open-source, local-first AI learning app for Windows, macOS and Linux.**

Build a subject knowledge map, explore concepts with formulas and interactive diagrams, and check your understanding with notes, quizzes and teach-back practice.

[![Release](https://img.shields.io/github/v/release/porridgeowefish/Learn-With-AI)](https://github.com/porridgeowefish/Learn-With-AI/releases/latest)
[![CI](https://github.com/porridgeowefish/Learn-With-AI/actions/workflows/ci.yml/badge.svg)](https://github.com/porridgeowefish/Learn-With-AI/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](./LICENSE)

**[Download for Windows, macOS and Linux](https://github.com/porridgeowefish/Learn-With-AI/releases/latest)** · [Product website](https://porridgeowefish.github.io/Learn-With-AI/en/) · [Quick start](#quick-start) · [Screenshots](#the-learning-workflow) · [Build from source](#build-from-source)

The bundled example needs no API key. New conversations and generated content require a configured model connection; provider charges may apply.

</div>

## What can I do with it?

Learn-With-AI is a desktop study workspace for moving from reading an explanation to explaining it yourself. Start with a question or bring your own textbook, lecture notes or slides.

| Learning task | What the app provides |
| --- | --- |
| Find a starting point | Subject maps with topic relationships, prerequisites and learning priorities |
| Understand a concept | AI tutor conversations with text, formulas, tables, diagrams and interactive examples |
| Study your own materials | PDF, Markdown, TXT, PPTX, DOCX, XLSX and CSV import, local parsing and source references |
| Work with scanned pages | Local OCR where supported by the operating system; check formulas against the original |
| Keep useful explanations | Automatic notes, editable review guides, version history and text annotations |
| Test your understanding | Generated quizzes with objective-question scoring, answers and explanations |
| Explain it yourself | Teach-back practice: explain a concept and let the tutor ask follow-up questions |
| Collect small discoveries | Start with an empty collection, receive a random gem after your first completed exchange, then discover 50 gems and shells through learning. View owned treasures in 3D; deleting their source conversation keeps the collection |

Learning files stay on your computer. You choose the model connection, including OpenAI- and Anthropic-compatible APIs. Local-first describes storage and local processing; model generation can use a remote service.

## Quick start

1. Download an installer from [Releases](https://github.com/porridgeowefish/Learn-With-AI/releases/latest).
2. Open the app and choose the bundled example (`体验示例`). Explore the gradient-descent example, review and quizzes without an API key. The example uses preset content and resets when closed.
3. To create your own learning content, add a model connection in Settings (`设置`), enter your provider's key, test the connection and save it.
4. Create a learning unit for a question, or a subject map for a broader field. Import your reference material if needed.

This guide is in English. Current screenshots and the bundled example use Chinese; this page does not promise a fully translated app interface.

| System | Package | How it opens |
| --- | --- | --- |
| Windows 10/11 x64 or ARM64 | `…-windows-<arch>.msi` | Native window; requires Microsoft WebView2 Runtime |
| macOS Intel or Apple Silicon | `…-darwin-<arch>.pkg` or `…-app.tar.gz` | Local interface in your default browser |
| Debian / Ubuntu | `…-linux-<arch>.deb` | Install with `sudo apt install ./package.deb` |
| Other Linux distributions | `…-linux-<arch>-install.tar.gz` | Extract and run `./install.sh` |

Choose `amd64` for x64 or `arm64` for ARM. Release packages include the frontend, backend and document parser; end users do not need Go, Node.js, Python or Office. Releases include `SHA256SUMS` for download verification. Windows packages are currently unsigned and may trigger a system warning; verify the official download source.

## The learning workflow

**Bring material → map the subject → explore → review → practise → teach it back.**

The following screenshots use an automatic-control course. The textbook names, conversations and learning content are authored demo data, not live model results. Generated explanations and visualizations depend on the model and the question.

### 1. See where a topic fits

Build a map around a subject or around the recognized text from your materials. Select a topic to add it to a plan or open a learning unit.

![Subject knowledge map with prerequisites, priorities and study actions](docs/images/discipline-map.png)

### 2. Explore ideas with the tutor

Ask for a new example, missing prerequisites or a visual explanation. Supported output includes mathematical notation, Mermaid diagrams, SVG illustrations and interactive HTML. Built-in JSXGraph controls can display 2D functions and rotatable 3D surfaces.

![Tutor explains open-loop and closed-loop control with a feedback diagram](docs/images/teacher.png)

![Interactive mathematical view with a surface, gradient and parameter controls](docs/images/structured-math.png)

The mathematical screenshot uses a fixed function and analytic gradient in the real renderer, without a model request.

### 3. Review and check your understanding

Keep notes, create review guides and annotate text. Attempt a quiz before revealing the answers and explanations. In teach-back practice, explain the idea in your own words and ask for feedback.

![Quiz feedback showing correct and incorrect responses](docs/images/practice.png)

![Teach-back conversation that checks an explanation of feedback control](docs/images/feynman.png)

## Data and model boundaries

- Projects, conversations, imported materials, notes and configuration are stored as ordinary local files. Updates and uninstalling preserve learning data.
- The app does not require an account with a Learn-With-AI cloud service. Live tutoring and generation need a working connection to the model provider you select.
- Local parsing and local OCR do not need to upload documents. Tutoring, Ask AI, generation and optional cloud recognition send the necessary content to your configured services.
- Model output, quiz answers and OCR can be wrong. Check important claims and formulas against your sources.
- API keys are stored in the local workspace's `config.local.json`. Do not commit this file or share it in an issue.

Default data locations: Windows `%LOCALAPPDATA%\learn-with-ai`; macOS `~/Library/Application Support/learn-with-ai`; Linux `$XDG_DATA_HOME/learn-with-ai` or `~/.local/share/learn-with-ai`. The full path is shown in Settings. On Windows, closing the window hides it to the tray; use the tray menu to exit.

## Build from source

Requires Go ≥ 1.26 and Node.js ≥ 20.19.

```bash
git clone https://github.com/porridgeowefish/Learn-With-AI.git
cd Learn-With-AI
npm install --prefix frontend
npm run build
npm run start
```

The source server opens `http://127.0.0.1:8787/`. Development data defaults to the ignored `workspace/` directory; set `--workspace` or `LLL_WORKSPACE_ROOT` to choose another location. The backend uses Go; the frontend uses React 18, TypeScript and Vite. See the [Chinese guide](./README.md#从源码构建) for development commands.

## Feedback and contributions

The repository was formerly named LLL_life_long_learn. Existing repository links redirect to Learn-With-AI; the username and current repository URL remain unchanged.

[Report a bug or suggest an improvement](https://github.com/porridgeowefish/Learn-With-AI/issues). Include the app version, operating system and steps to reproduce the problem; remove keys and private learning content. See [CONTRIBUTING.md](./CONTRIBUTING.md) for the contribution workflow.

If this workspace helps your learning, a Star makes it easier to find again. A concrete example of what worked or where you got stuck is also welcome.

[MIT](./LICENSE) © 2026 Xiong Minzhe
