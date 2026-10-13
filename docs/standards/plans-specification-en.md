# Plans & Features Specification

## 1. Objective & Scope
This specification standardizes the numbering rules, hierarchical structure, authoring principles, and maintenance mechanisms for plan documents in `url-proxy`. It ensures documentation remains clear, cohesive, evolutionary, and resilient against codebase refactoring and directory reorganizations.

---

## 2. Numbering & Hierarchy

All plan documents reside in `docs/plans/` using a flat naming pattern: `<number>-<semantic-slug>-{zh,en}.md`.

### 2.1 Master Plans (xx0)
- **Numbering Pattern**: Ends with `0` (e.g., `100`, `200`, `300`, `310`, `320`).
- **Role & Responsibilities**:
  - Serves as the high-level lead document for the evolutionary stage;
  - Articulates stage background, core pain points, objectives, and architectural decisions;
  - Provides the sub-plan evolution matrix and delivery roadmap;
  - Establishes global configuration directives and cross-cutting protocol mechanisms introduced in the stage.
- **Progression**: Advanced functional stages increment by `+10` (e.g., 310 advances to 320, then 330).

### 2.2 Sub-plans & Features (xx1 ~ xx9)
- **Numbering Pattern**: Ends with a non-zero digit (e.g., `101`, `102`, `301`, `311`, `321`).
- **Role & Responsibilities**:
  - Subordinated under the corresponding master plan (e.g., 311~316 under Plan 310);
  - Focuses on detailed technical specifications, protocols, and behavioral logic for a single feature or subsystem;
  - Outlines directives, execution pipelines, edge cases, and automated verification criteria.

---

## 3. Authoring & Decoupling Principles

To prevent technical documentation from accumulating broken links and maintenance drag as code refactors occur, all plan documents must strictly adhere to the following principles:

### 3.1 Avoid Binding to Physical Source Code Paths
- **No hardcoded source paths**: Plan documents define architecture and protocol behavior. Do not hardcode source file paths (such as `.go` or `.c` files) in text, titles, or headers.
- **Use abstract component and domain terminology**: Refer to functional areas via logical names:
  - "Unified Configuration Engine (Config)"
  - "Cache Manager (Cache)"
  - "Concurrent Downloader (Downloader)"
  - "Streaming Response Rewriter (Rewriter)"
  - "Proxy Dispatcher Core (Proxy)"
  - "Main Assembler / Entrypoint"

### 3.2 Avoid Cross-Document Hyperlinks
- **No fragile markdown file links**: Do not use Markdown file relative links such as `Feature 311 (path/to/311-xxx.md)` or `Feature 304 (path/to/304-xxx.md)`. These break when files are organized or renamed.
- **Reference by logical identifier**: Reference cross-cutting plans and features by clean identifiers:
  - `Feature 311 (Streaming Response Body Rewriter)`
  - `Feature 304 (Unified Configuration System)`
  - `Plan 300 Stage`

### 3.3 Formatting and Compliance
- **No absolute paths or protocol URLs**: Absolute host paths, drive letters, WSL mount paths, and `file://` URLs are strictly forbidden.
- **Relative repository paths for resources**: Use clean repository relative paths for configs and test suites (e.g., `proxy.conf`, `tests/304_unified_config/`).
- **No hyperlinks on code symbols**: Enclose configs and symbols in inline code ticks without hyperlink wrappers (e.g., `proxy.conf`).
- **Mandatory bilingual pairing**: Every plan document must be maintained in both Chinese (`-zh.md`) and English (`-en.md`).
