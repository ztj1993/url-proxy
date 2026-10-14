# 功能开发规范标准 (Feature Development Specification)

## 1. 目标与定位 (Objective & Scope)
本规范用于标准化 `url-proxy` 项目功能的完整研发交付流程，涵盖方案定义、逐个子方案实施、测试构建、代码审计与死代码/冗余代码消除、独立缺陷修复、全量回归与阶段收尾等环节，确保代码实现、测试套件与技术文档高内聚、原子化且可精准追溯。

---

## 2. 功能开发生命周期 (Development Lifecycle)

### 2.1 阶段准备：方案定义先行 (Planning Phase)
- **文档创建**：在编写任何业务源码前，必须先在 `docs/plans/` 目录下完成当前阶段主方案（`xx0-<name>-{zh,en}.md`）与所有下属子方案（`xx1`~`xx9`）的双语设计文档编写。
- **内容要求**：严格遵循 `docs/standards/plans-specification-zh.md`，使用逻辑模块命名，禁止绑定物理源码路径，禁止跨方案超链接。
- **暂存策略**：方案定义完成后**暂不执行 Git 提交**，文件保持在工作区未暂存状态，后续随实施节奏逐阶段提交。

### 2.2 子方案实施单元：测试构建、代码审计与独立提交 (Implementation, Code Review & Atomic Commit)
每个子方案（`xx1`~`xx9`）作为一个严格自治的交付单元，必须按序完成以下流程：
1. **测试套件构建**：在 `tests/` 目录下创建对应的子方案独立测试套件目录 `tests/<编号>_<英文语义名>/`。
2. **业务功能实现**：在 `internal/` 领域包内编写纯 Go 标准库实现，严格遵循依赖注入与状态隔离原则。
3. **单方案测试验证**：运行该子方案测试，确保验证通过。
4. **代码审计与冗余消除 (Code Review & Redundancy Elimination)**：
   - **差异逐行审查 (Diff Analysis)**：在执行 Git 提交前，必须运行 `git diff` 对本次子方案的所有新增与修改代码进行逐行审查。
   - **死代码与无用定义消除 (Dead & Unused Code Elimination)**：彻底清理未引用的变量、常量、结构体字段（特别是只写未读字段）、未导出的死方法与无用类型；清除不可达逻辑分支与恒假/恒真/无操作条件。
   - **重复逻辑与多余包装简化 (Duplication & Wrapper Simplification)**：杜绝就地复制代码，复用已有方法或提取共享逻辑；移除无业务价值的多层套娃包装函数与过度封装。
   - **调试与脚手架清理 (Debug & Scaffolding Cleanup)**：彻底移除所有临时调试日志（如 `log.Printf`、`fmt.Println`）与开发期打桩代码。
   - **清理后测试重验 (Post-Cleanup Re-verification)**：精简与清理完成后，必须重新运行该子方案测试套件（`go test -v -count=1 ./tests/<编号>_<英文语义名>/...`），确保在消除冗余后测试依然 100% 通过。
5. **子方案独立 Git 提交**：
   - 包含内容：该子方案设计文档（`docs/plans/<编号>-*-{zh,en}.md`）+ 业务源码改动 + 对应的测试套件目录（`tests/<编号>_<英文语义名>/`）。
   - 提交信息：统一使用纯英文格式，明确标注对应的 Feature 编号。
   - 禁止将未经代码审计、残留冗余或死代码的改动带入提交，且严禁将多个子方案的代码或测试混杂在同一个提交中。

### 2.3 阶段回归与独立缺陷修复 (Regression & Defect Isolation)
当该阶段主方案下的所有子方案全部完成独立提交后，进入阶段回归与守护流程：
1. **全量测试回归**：在收尾提交前，必须全量执行 `tests/` 目录下的所有测试套件。
2. **缺陷阻断原则**：若全量回归中有任何测试用例失败，严禁执行阶段收尾提交，必须先完成缺陷修复。
3. **缺陷独立提交**：每一个缺陷的修复代码及其实验验证必须**单独执行独立的 Git 提交**，严禁与阶段收尾文档或其它子方案提交合并。
4. **跨方案冗余与遗留清理**：在全量测试通过后，对阶段内跨子方案的代码进行整体交叉审计，识别并消除因方案演进产生的跨模块重复调用、废弃字段或残留逻辑；清理修改必须单独提交并重验全量测试。

### 2.4 阶段收尾与文档边界 (Stage Finalization & Documentation Boundary)
在所有子方案已独立提交、全量测试回归 100% 通过且所有缺陷修复均已单独提交后，执行阶段收尾：
1. **文档边界控制**：在特性开发阶段和阶段收尾过程中，仅允许修改当前方案文档，严格保持文档改动的范围高内聚。
2. **阶段收尾 Git 提交**：
   - 包含内容：该阶段主方案设计文档（`docs/plans/xx0-*-{zh,en}.md`）。
   - 提交信息：统一使用纯英文格式，注明阶段主方案编号及主要成果。

### 2.5 主方案后续补充与附录规范 (Post-Submission Master Plan Addendum)
- **主体冻结原则**：若阶段主方案已经完成 Git 提交，后续若需要进行功能补充、架构扩展或缺陷修复，**严禁修改主方案已有的主体内容**。
- **附录追加形式**：所有后续功能补充与缺陷修复说明，必须以在主方案文末追加“附录 (Appendix)”的形式实现，确保历史架构方案的严肃性与时间线追溯性。

---

## 3. 测试套件规范 (`tests/` 目录) (Test Suite Specification)

每一个子方案必须在 `tests/` 目录下拥有独立的测试环境与套件目录。

### 3.1 目录命名格式
`tests/<子方案三位数字编号>_<英文下划线语义名>/`（例如 `tests/331_ttl_cache_eviction/`）。

### 3.2 套件组成要素
每个测试套件目录下必须包含以下要素：
- `README-zh.md` 与 `README-en.md`：中英双语测试指南，包含测试目标、套件结构、自动化执行命令及手工验证步骤。
- `mock_server.go`（若适用）：模拟上游依赖服务的协议交互、返回头及状态码。
- `*_test.go`：基于 Go 标准库 `testing` 包的自动化测试用例，覆盖正常流、边界条件与错误路径。
- `proxy_test.conf`（若适用）：该子方案专属的代理配置文件。

### 3.3 测试执行命令
- **单子方案测试命令**：
  ```bash
  go test -v -count=1 ./tests/<编号>_<英文语义名>/...
  ```
- **全量回归测试命令**：
  ```bash
  go test -v -count=1 ./tests/...
  ```

---

## 4. Git 提交规范 (Git Commit Standards)

### 4.1 语言格式
- 生成 Git 提交消息时，必须同时输出中文与英文两个版本供查阅；
- 实际执行 Git 提交时，统一仅使用英文格式，严格遵循 Conventional Commits 约定式规范（如 `feat`、`fix`、`docs`、`refactor`、`test`）。

### 4.2 提交分类与范例
- **子方案交付提交**（必须携带 `Feature <编号>` 标记）：
  ```text
  feat(cache): implement ttl cache eviction and capacity limit (Feature 331)
  ```
- **缺陷修复提交**（每一个缺陷独立提交，标注对应 Feature 或模块）：
  ```text
  fix(cache): resolve race condition in lru eviction queue (Feature 331)
  ```
- **独立清理与重构提交**（标注对应 Feature 或模块）：
  ```text
  refactor(proxy): eliminate redundant wrapper functions and unused struct fields (Feature 335)
  ```
- **阶段收尾提交**（仅提交阶段主方案设计文档）：
  ```text
  docs(plans): complete stage 330 cache governance plan
  ```
