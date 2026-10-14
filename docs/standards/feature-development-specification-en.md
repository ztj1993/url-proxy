# Feature Development Specification

## 1. Objective & Scope
This specification standardizes the end-to-end development and delivery lifecycle for features in the `url-proxy` project. It covers initial planning, sub-plan implementation, test suite construction, code review and dead/redundant code elimination, isolated defect remediation, full regression testing, and stage finalization, ensuring that implementations, test suites, and documentation remain cohesive, atomic, and fully traceable.

---

## 2. Feature Development Lifecycle

### 2.1 Planning Phase: Specifications First
- **Document Creation**: Before writing any implementation code, the bilingual design documents for the current stage master plan (`xx0-<name>-{zh,en}.md`) and all subordinate sub-plans (`xx1` to `xx9`) must be created under `docs/plans/`.
- **Content Requirements**: Strictly adhere to `docs/standards/plans-specification-en.md`. Use logical module names, prohibit binding to physical source paths, and avoid cross-plan hyperlinks.
- **Commit Strategy**: Do not commit plan documents immediately upon creation. Keep them unstaged in the working tree and commit them incrementally alongside the implementation stages.

### 2.2 Sub-plan Implementation: Testing, Code Review, and Atomic Commits
Each sub-plan (`xx1` to `xx9`) acts as a strictly self-contained delivery unit and must proceed in order:
1. **Build Test Suite**: Create an isolated test suite directory under `tests/` named `tests/<id>_<semantic_name>/`.
2. **Implement Business Logic**: Write pure Go standard library implementations in `internal/` domain packages, strictly following dependency injection and state isolation principles.
3. **Verify Sub-plan Test**: Execute tests for the sub-plan to ensure all checks pass.
4. **Code Review & Redundancy Elimination**:
   - **Line-by-line Diff Analysis**: Run `git diff` before executing Git commits to perform a line-by-line review of all modified and newly introduced code.
   - **Dead & Unused Code Elimination**: Completely eliminate unreferenced variables, constants, struct fields (specifically write-only fields), unexported dead methods, and unused types; remove unreachable branches and no-op conditions.
   - **Duplication & Wrapper Simplification**: Avoid duplicating logic in multiple places; reuse existing module methods or extract shared helpers; remove unnecessary multi-layer wrapper functions.
   - **Scaffolding & Debug Removal**: Strip out all temporary debugging prints (e.g., `log.Printf`, `fmt.Println`), stubs, and developmental scaffolding.
   - **Post-Cleanup Test Re-verification**: Re-run the sub-plan test suite after cleanup (`go test -v -count=1 ./tests/<id>_<semantic_name>/...`) to ensure all tests pass 100% and no regressions were introduced.
5. **Isolated Sub-plan Git Commit**:
   - Contents: The sub-plan design documents (`docs/plans/<id>-*-{zh,en}.md`) + implementation source code changes + corresponding test suite directory (`tests/<id>_<semantic_name>/`).
   - Commit Message: Must be in English only and explicitly reference the corresponding Feature identifier.
   - Committing unaudited changes or code carrying dead/redundant logic is strictly prohibited, as is mixing code or tests from multiple sub-plans into a single commit.

### 2.3 Stage Regression and Defect Isolation
Once all sub-plans under the master plan have been committed independently, proceed to stage regression and defect verification:
1. **Full Regression Execution**: Run all test suites under the `tests/` directory prior to any finalization commit.
2. **Blocking Guard**: If any test fails during full regression, stage finalization is strictly blocked until defects are addressed.
3. **Isolated Bugfix Commits**: Every bugfix and its accompanying verification must be committed in a standalone Git commit. Never combine bugfixes with stage finalization documents or other sub-plan commits.
4. **Cross-Plan Redundancy & Legacy Cleanup**: After full regression tests pass, perform a cross-subplan code quality audit. Identify and eliminate cross-module duplicate calls, unreferenced fields, or obsolete logic resulting from plan evolution. All cleanups must be committed independently and re-verified against full regression tests.

### 2.4 Stage Finalization and Documentation Boundary
After all sub-plans are committed, full regression tests pass at 100%, and all bugfixes are committed individually, finalize the stage:
1. **Documentation Boundary Control**: During feature development and stage finalization, only modifications to the current plan documents are permitted, maintaining tight cohesion in documentation modifications.
2. **Stage Finalization Git Commit**:
   - Contents: Master plan design documents (`docs/plans/xx0-*-{zh,en}.md`).
   - Commit Message: Must be in English only, referencing the master plan identifier and major outcomes.

### 2.5 Post-Submission Master Plan Addendum
- **Main Body Freezing Principle**: Once the stage master plan has been committed to Git, modifying its existing main body is strictly prohibited for subsequent feature additions, architectural expansions, or bugfixes.
- **Appendix/Addendum Format**: All subsequent feature supplements and defect fixes must be recorded by appending an "Appendix" section at the end of the master plan, ensuring the integrity and historical traceability of architectural decisions.

---

## 3. Test Suite Specification (`tests/` Directory)

Every sub-plan must have its own isolated test environment and suite directory under `tests/`.

### 3.1 Directory Naming Convention
`tests/<three-digit-id>_<snake_case_english_name>/` (for example, `tests/331_ttl_cache_eviction/`).

### 3.2 Required Components
Each test suite directory must contain:
- `README-zh.md` & `README-en.md`: Bilingual test guides detailing testing objectives, directory layout, automated test commands, and manual verification steps.
- `mock_server.go` (if applicable): Simulates upstream service protocol interactions, response headers, and status codes.
- `*_test.go`: Automated tests written with Go's standard library `testing` package, covering standard flows, edge cases, and failure paths.
- `proxy_test.conf` (if applicable): Dedicated proxy configuration file for the sub-plan.

### 3.3 Test Execution Commands
- **Sub-plan Test Command**:
  ```bash
  go test -v -count=1 ./tests/<id>_<semantic_name>/...
  ```
- **Full Regression Test Command**:
  ```bash
  go test -v -count=1 ./tests/...
  ```

---

## 4. Git Commit Standards

### 4.1 Language & Formatting
- When generating Git commit messages, provide both Chinese and English versions for reference;
- When executing actual Git commits, use English only, strictly following Conventional Commits format (`feat`, `fix`, `docs`, `refactor`, `test`).

### 4.2 Categories and Examples
- **Sub-plan Delivery Commit** (must include the `Feature <id>` tag):
  ```text
  feat(cache): implement ttl cache eviction and capacity limit (Feature 331)
  ```
- **Bugfix Commit** (one standalone commit per defect, referencing the Feature or module):
  ```text
  fix(cache): resolve race condition in lru eviction queue (Feature 331)
  ```
- **Cleanup & Refactoring Commit** (referencing the Feature or module):
  ```text
  refactor(proxy): eliminate redundant wrapper functions and unused struct fields (Feature 335)
  ```
- **Stage Finalization Commit** (commits master plan design documents only):
  ```text
  docs(plans): complete stage 330 cache governance plan
  ```
