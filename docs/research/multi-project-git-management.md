# 多项目 Git 管理方案调研：monorepo、submodule、subtree 与 worktree

> 调研日期：2026-08-13  
> 资料范围：仅使用 Git 官方文档、GitHub 官方文档和官方工程资料。  
> 证据约定：`已验证事实`是来源直接说明的内容；`推荐`和`推论`是基于这些事实给出的工程判断，不把官方未明确表述的经验写成事实。

## 结论先行

如果前端、后端和共享代码需要经常一起修改、一起评审，并且团队希望一次提交表达一个跨项目变更，**推荐先采用 monorepo**：一个 Git 仓库，按目录划分项目，再用 CODEOWNERS、路径过滤和按目录运行的 CI 管理边界。GitHub 将仓库定义为包含代码、文件及其修订历史的基本单元；因此这种方案的核心是让多个项目共享同一个版本历史和协作边界。[GitHub：About repositories](https://docs.github.com/en/repositories/creating-and-managing-repositories/about-repositories?apiVersion=2022-11-28)

如果项目需要独立权限、独立发布节奏或天然是可复用的外部组件，则选择**多个独立仓库**；需要在本地同时打开多个分支时，在每个独立仓库内部使用 `git worktree`。`worktree` 是一个仓库的多个工作树，不是跨多个仓库的依赖管理机制。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)

`git submodule` 适合“主项目依赖另一个需要保持独立历史、并且要锁定到明确提交”的关系；`git subtree` 适合“把另一个项目的代码纳入主仓库目录，并让普通使用者不必额外初始化嵌套仓库”的关系。它们不是 monorepo 与独立仓库之间的普适替代品，而是不同的集成边界。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)；[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)

## 1. 问题范围与术语

### 1.1 本文回答的问题

本文讨论以下场景：一个本地父文件夹中同时放置前端、后端、共享库、基础设施或文档等多个项目；团队希望明确 Git 仓库边界、历史边界、发布边界、权限边界和本地并行开发方式。这里的“一个文件夹”不必等于“一个 Git 仓库”：父文件夹可以只是工作区容器。

本文比较四类方案：

1. **Monorepo**：多个项目目录由一个 Git 仓库统一跟踪。
2. **Git submodule**：一个 superproject 在目录中挂载另一个独立仓库，并记录该仓库的特定提交。
3. **Git subtree**：把另一个项目合并到主仓库的子目录，可选择保留完整历史，也可使用 `--squash` 压缩导入历史。
4. **多个独立仓库 + Git worktree**：每个项目拥有自己的仓库；`worktree` 只用于同一仓库同时检出多个分支或提交。

### 1.2 重要边界

- Git 官方文档直接定义了仓库、submodule、subtree 和 worktree，但没有发布一套名为“monorepo 最佳实践”的 Git 标准。本文对 monorepo 的目录、CI、CODEOWNERS 等内容是基于官方能力的推荐组合，不是 Git 的强制规则。
- “项目是否应该共享版本号、权限、发布节奏、原子提交”是团队架构决策，不能仅凭 Git 命令决定。
- 供应链、构建系统、包管理器和部署平台的约束不由 Git 本身解决；本调研只覆盖源代码组织与 Git 协作边界。

## 2. 已验证事实

### 2.1 仓库与 monorepo

- GitHub 的官方定义是：仓库保存代码、文件及每个文件的修订历史；仓库可有多个协作者，且可以公开或私有。[GitHub：About repositories](https://docs.github.com/en/repositories/creating-and-managing-repositories/about-repositories?apiVersion=2022-11-28)
- Google 的官方工程资料把其代码库描述为单一仓库，并明确讨论了这种模式的收益与权衡；这说明“大规模单仓库”是一个真实的工程模式，但该资料描述的是 Google 自建的配套工具与流程，不能直接当作普通 Git 仓库的性能保证。[Google Research：Why Google Stores Billions of Lines of Code in a Single Repository](https://research.google/pubs/why-google-stores-billions-of-lines-of-code-in-a-single-repository/)
- GitHub 官方说明，目录树过深会使历史遍历操作变慢；仓库资源也存在平台限制。因此 monorepo 的规模风险应通过实际仓库规模、历史大小、CI 时间和开发工具表现验证，而不能只依据“一个仓库”这个标签判断。[GitHub：Repository limits](https://docs.github.com/en/repositories/creating-and-managing-repositories/repository-limits)

### 2.2 Submodule

- Git 官方将 submodule 定义为“嵌入另一个仓库的仓库”；被嵌入的仓库有自己的历史，外层称为 superproject。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- superproject 通过 tree 中的 `gitlink` 记录 submodule 期望的提交，并通过 `.gitmodules` 提供名称、路径和 URL 等映射信息。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- Git 官方列出的用途包括：保持另一个项目独立历史、把 superproject 固定到任意版本，以及通过拆分仓库降低不相关内容的获取量或实施不同访问策略。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- 克隆或拉取含 submodule 的仓库时，submodule 默认不会自动检出；可以用 `git clone --recurse-submodules`，或随后运行 `git submodule update --init`，嵌套 submodule 则增加 `--recursive`。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- `git submodule update` 的默认 checkout 流程会把 submodule 检出到 superproject 记录的提交，且该提交通常处于 detached `HEAD`；也可以显式选择 rebase 或 merge 流程。[Git：git-submodule](https://git-scm.com/docs/git-submodule)

### 2.3 Subtree

- Git 官方维护的 `git-subtree` 文档说明：subtree 把子项目放入主项目的一个子目录，可选择包含子项目的完整历史。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- 同一文档明确区分 subtree 与 submodule：subtree 不需要 `.gitmodules` 或 gitlink，普通用户不需要额外理解或初始化嵌套仓库；它在主仓库中表现为可提交、可分支、可合并的普通子目录。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- `git subtree` 支持 `add`、`merge`、`pull`、`push` 和 `split`；因此可以把外部仓库导入子目录，也可以把子目录历史拆分成独立项目。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- `--squash` 会把导入或合并的子项目变化压成一个提交；不使用该选项会导入更多子项目提交。这个选择影响主仓库历史的详细程度和体积，应由团队明确约定。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- GitHub 官方的 subtree merge 指南也把这种方式描述为：子仓库保存在主仓库的一个文件夹中，并可按示例从子项目获取更新。[GitHub：About Git subtree merges](https://docs.github.com/en/get-started/using-git/about-git-subtree-merges)

### 2.4 Git worktree

- Git 官方将 `git worktree` 定义为管理“附属于同一仓库的多个工作树”；一个非 bare 仓库有一个主工作树和零个或多个 linked worktree。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- `git worktree add` 可以为一个路径创建或检出分支；`list`、`remove`、`move`、`lock`、`prune` 和 `repair` 用于查看和维护这些工作树。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- 多 worktree 时，部分 refs 在所有 worktree 间共享，而 `HEAD` 等伪引用按 worktree 区分。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- 因而，“多个独立仓库配合 worktree”实际是两层结构：独立仓库解决项目/权限/历史边界，worktree 解决每个仓库内部的并行工作目录；Git 官方没有把 worktree 定义为多个仓库之间的联动机制。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)

### 2.5 Monorepo 的协作治理能力

- GitHub 的 CODEOWNERS 文件可以按文件或目录指定代码所有者；当拉取请求修改其负责的代码时，GitHub 可以自动请求相应团队审查。[GitHub：About code owners](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners)
- GitHub Actions 支持用 `paths` 或 `paths-ignore` 按文件路径过滤 workflow；同时使用分支过滤和路径过滤时，两个条件都必须满足。[GitHub：Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- GitHub 明确提醒：如果一个必需检查因路径过滤而被跳过，该检查可能保持 Pending，从而阻塞依赖该检查的合并。[GitHub：Skipping workflow runs](https://docs.github.com/actions/managing-workflow-runs/skipping-workflow-runs)
- GitHub rulesets/branch protection 可以要求审查和状态检查通过后才能合并；这为单仓库中的目录级责任和统一质量门禁提供了平台能力。[GitHub：Managing a branch protection rule](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/managing-a-branch-protection-rule)

## 3. 方案比较

| 方案 | Git 边界 | 依赖/版本表达 | 本地使用体验 | 适合情况 | 主要代价与边界 |
| --- | --- | --- | --- | --- | --- |
| Monorepo | 一个仓库、一个统一历史 | 目录内代码可以在同一提交中一起变更；具体依赖版本由构建/包管理工具决定 | 克隆一次，项目目录直接可见；可配合 sparse-checkout 缩小工作树。[Git：sparse-checkout](https://git-scm.com/docs/git-sparse-checkout) | 前后端强耦合、共享库频繁同步、需要跨项目原子 PR、团队愿意统一权限和治理 | 仓库与 CI 规模增长；需要目录边界、CODEOWNERS、路径 CI 和构建缓存；GitHub 的仓库限制必须实测 |
| Git submodule | 多个独立仓库；主仓库记录 gitlink | 明确锁定到 submodule 的某个提交 | 初次克隆和更新需要额外初始化；进入 submodule 后是独立 Git 仓库 | 外部库、独立发布组件、需要独立权限/历史、主项目只在指定版本升级 | 容易忘记递归初始化或提交 superproject 指针；默认更新可能是 detached `HEAD`；跨仓库原子变更需要协调多个提交 |
| Git subtree | 主仓库内的普通子目录，可与外部仓库双向同步 | 可保留完整历史或用 `--squash`；同步由约定的 subtree 命令驱动 | 使用者看到的是普通目录，不必初始化 submodule | 需要把外部项目“带进来”并降低使用者操作成本，同时保留未来拆分/同步能力 | 同步方向、prefix、是否 squash 必须统一；合并冲突和历史映射需要维护者处理；主仓库仍会承载导入内容 |
| 多独立仓库 + worktree | 每个项目一个仓库；worktree 只属于各自仓库 | 每个仓库独立提交、分支、标签和发布；跨项目关系需另行记录 | 可在同一父文件夹并排放置多个项目，并在单个仓库同时打开多个分支 | 权限、发布、团队、生命周期明显独立；需要并行修复多个分支 | 没有跨仓库原子提交；跨项目变更需要多个 PR/版本协调；worktree 不替代依赖管理 |

表中的“适合情况”和“主要代价”是基于上节已验证事实作出的工程推论，不是 Git/GitHub 对组织形态的强制推荐。

## 4. 推荐决策

### 4.1 默认决策树（推荐）

1. **前端、后端、共享库是否经常需要同一个变更一起提交和一起通过 CI？**
   - 是：优先 monorepo。
   - 否：继续判断独立权限和发布边界。
2. **项目是否需要独立仓库权限、独立发布节奏或独立生命周期？**
   - 是：优先多个独立仓库；本地并行开发用 worktree 辅助。
   - 否：monorepo 通常是更简单的默认起点（这是推荐/推论）。
3. **主项目是否只是消费一个要锁定版本的外部仓库？**
   - 是：考虑 submodule。
4. **是否希望外部项目代码在主仓库中表现为普通目录，并且不要求使用者初始化嵌套仓库？**
   - 是：考虑 subtree。

### 4.2 针对“前端 + 后端”的建议

在没有额外团队约束的情况下，推荐采用如下基线：

- 用一个 monorepo 管理前端、后端、共享契约和仓库级文档。
- 以目录作为责任边界，用 CODEOWNERS 指定前端、后端、基础设施和共享代码的评审团队。[GitHub：About code owners](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners)
- 为前端、后端和共享代码配置独立 workflow；使用路径过滤减少无关 CI，但要确保必需检查不会因为被跳过而长期 Pending。[GitHub：Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)；[GitHub：Skipping workflow runs](https://docs.github.com/actions/managing-workflow-runs/skipping-workflow-runs)
- 只有在实测仓库规模、权限或发布边界不适合单仓库时，才拆成多个独立仓库；拆分后用明确的版本/契约发布流程管理跨仓库兼容性（这是推荐/推论）。
- 不把 worktree 当作项目拆分方案；它只解决同一仓库的并行分支工作目录。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)

## 5. 建议目录结构

### 5.1 推荐的 monorepo

```text
product-repo/
├── apps/
│   └── frontend/              # 前端应用
├── services/
│   └── backend/               # 后端服务
├── packages/
│   └── shared-contract/       # 前后端共享契约/类型/客户端
├── infra/                     # 部署和基础设施配置
├── docs/                      # 项目文档与研究笔记
├── .github/
│   ├── CODEOWNERS             # 目录责任与评审路由
│   └── workflows/
│       ├── frontend.yml
│       ├── backend.yml
│       └── shared.yml
├── README.md
└── .gitignore
```

这是目录和治理的建议模板，不是 Git 的规定。目录应按构建边界、责任边界和部署边界调整；不要为了“看起来像 monorepo”而人为引入空的共享层。

### 5.2 多独立仓库 + worktree 的父文件夹

```text
workspace/
├── frontend/                  # 独立 Git 仓库
│   ├── .git/
│   └── ...
├── backend/                   # 独立 Git 仓库
│   ├── .git/
│   └── ...
├── shared-contract/           # 可选：独立仓库或发布包
└── worktrees/
    ├── frontend-hotfix/       # frontend 仓库的 linked worktree
    └── backend-release/       # backend 仓库的 linked worktree
```

父文件夹 `workspace/` 只是本地组织方式；除非显式初始化 Git，否则它不是仓库。每个 worktree 的管理命令应在对应仓库中执行。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)

### 5.3 Submodule 的形态

```text
platform/
├── app/
├── vendor/
│   └── shared-contract/       # submodule 工作树
├── .gitmodules
└── ...
```

外层仓库提交的是 submodule 的 gitlink 和 `.gitmodules` 映射，而不是把 submodule 的每个文件作为外层普通文件提交。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)

### 5.4 Subtree 的形态

```text
platform/
├── apps/
├── services/
├── vendor/
│   └── shared-contract/       # 主仓库中的普通子目录
└── ...
```

在工作树表面，subtree 与普通目录相同；维护者通过约定的 `git subtree` 命令与来源仓库同步。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)

## 6. 常用命令

以下命令中的 URL、路径、分支名和提交号均为示例。执行前应确认目标路径和远端权限。

### 6.1 Monorepo：初始化、分支和按目录检出

```bash
# 新建一个单仓库
mkdir product-repo
cd product-repo
git init
git add .
git commit -m "chore: initialize repository"

# 为一次跨项目变更创建分支
git switch -c feature/frontend-backend-change

# 只在本地工作树中保留需要的目录（可选）
git sparse-checkout init --cone
git sparse-checkout set apps/frontend packages/shared-contract

# 查看状态并提交
git status
git add apps/frontend services/backend packages/shared-contract
git commit -m "feat: change frontend backend contract"
```

`git sparse-checkout` 的官方定义是按模式减少工作树中实际检出的文件；它适合大型单仓库中只关注部分目录的场景，但不等于把目录变成独立仓库。[Git：sparse-checkout](https://git-scm.com/docs/git-sparse-checkout)

### 6.2 Submodule：添加、初始化、更新和提交指针

```bash
# 在 superproject 中添加 submodule
git submodule add https://github.com/example/shared-contract.git vendor/shared-contract
git commit -m "build: add shared contract submodule"

# 新成员克隆并初始化全部嵌套 submodule
git clone --recurse-submodules https://github.com/example/platform.git platform

# 已有克隆：初始化并检出 superproject 记录的提交
git submodule update --init --recursive

# 查看当前 submodule 状态
git submodule status

# 将 submodule 更新到指定提交，然后提交 superproject 中的 gitlink 变化
git -C vendor/shared-contract fetch
git -C vendor/shared-contract checkout <commit-or-tag>
git add vendor/shared-contract
git commit -m "build: update shared contract"

# 移除 submodule（按 Git 官方命令路径）
git submodule deinit vendor/shared-contract
git rm vendor/shared-contract
git commit -m "build: remove shared contract submodule"
```

这些命令分别对应 Git 官方的 submodule 添加、初始化、更新、状态和移除流程。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)；[Git：git-submodule](https://git-scm.com/docs/git-submodule)

### 6.3 Subtree：导入、同步、推送和拆分

```bash
# 将远端项目以 subtree 导入，并只保留一个压缩提交
git subtree add --prefix=vendor/shared-contract \
  https://github.com/example/shared-contract.git main --squash

# 从远端拉取更新
git subtree pull --prefix=vendor/shared-contract \
  https://github.com/example/shared-contract.git main --squash

# 将主仓库中的 subtree 变化推回独立仓库
git subtree push --prefix=vendor/shared-contract \
  https://github.com/example/shared-contract.git main

# 将指定子目录拆成可发布到独立仓库的分支
git subtree split --prefix=vendor/shared-contract -b shared-contract-split
```

`--squash` 是否使用、同步方向、prefix 和远端分支都应写入团队文档；Git 官方文档明确说明了这些命令的作用及 squash 对历史的影响。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)

### 6.4 独立仓库 + worktree：并行检出

```bash
# 分别克隆独立仓库
git clone https://github.com/example/frontend.git frontend
git clone https://github.com/example/backend.git backend

# 在 frontend 仓库中为 hotfix 创建另一个工作树
git -C frontend worktree add ../worktrees/frontend-hotfix -b hotfix/frontend

# 在 backend 仓库中为 release 创建另一个工作树
git -C backend worktree add ../worktrees/backend-release -b release/backend

# 查看和清理
git -C frontend worktree list
git -C frontend worktree remove ../worktrees/frontend-hotfix
git -C frontend worktree prune
```

`worktree add`、`list`、`remove` 和 `prune` 均为 Git 官方支持的工作树管理命令。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)

## 7. 风险与边界

### Monorepo

- **规模风险**：GitHub 文档指出深目录树会使历史遍历变慢，并列出仓库资源限制；应监测 clone/fetch、索引、搜索、构建和 CI 时长。[GitHub：Repository limits](https://docs.github.com/en/repositories/creating-and-managing-repositories/repository-limits)
- **CI 误配置风险**：路径过滤可能跳过 workflow；若该检查是 required check，跳过后的 Pending 状态可能阻塞合并。[GitHub：Skipping workflow runs](https://docs.github.com/actions/managing-workflow-runs/skipping-workflow-runs)
- **责任边界风险**：一个仓库不自动产生目录级责任，需要 CODEOWNERS、评审规则和清晰的构建入口（推荐/推论）。[GitHub：About code owners](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners)
- **不是自动的多版本发布系统**：Git 统一历史不等于前端和后端必须共用版本号；版本、发布和兼容性策略仍需团队定义（推荐/推论）。

### Submodule

- **初始化门槛**：普通 clone 不会自动检出 submodule；新成员、CI 和发布脚本必须明确使用 `--recurse-submodules` 或 `update --init --recursive`。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- **指针更新门槛**：修改 submodule 后，还要在 superproject 中提交新的 gitlink；只提交 submodule 自己的提交不会自动更新 superproject（由 gitlink 机制直接推出，属推论）。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)
- **工作分支风险**：默认 update 会 checkout 到记录的提交，可能是 detached `HEAD`；在 submodule 内开发时应建立/切换明确分支，并按独立仓库流程提交和推送。[Git：git-submodule](https://git-scm.com/docs/git-submodule)
- **权限与可用性边界**：submodule URL 指向另一个仓库，访问者和 CI 必须拥有相应访问权限；GitHub 官方也说明某些托管场景对 private submodule 有额外限制。[Git：gitsubmodules](https://git-scm.com/docs/gitsubmodules.html)；[GitHub：Using submodules with GitHub Pages](https://docs.github.com/en/pages/getting-started-with-github-pages/using-submodules-with-github-pages)

### Subtree

- **同步协议风险**：subtree 在工作树中像普通目录，但维护者必须持续记住来源仓库、prefix、同步方向和 squash 约定；否则容易出现重复导入或难以解释的合并历史（推荐/推论）。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- **历史取舍**：不使用 squash 会导入更多历史；使用 squash 则主仓库中的历史细节更少。两种行为都由官方命令支持，但没有官方替团队选择的默认策略。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)
- **不是实时镜像**：导入之后主仓库目录不会自动随来源仓库变化；必须显式执行 pull/merge，推回来源也必须显式执行 push（由命令模型直接推出，属推论）。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)

### 多独立仓库 + worktree

- **跨仓库原子性边界**：worktree 只附属于一个仓库；它不能把前端和后端的多个仓库变成一个提交或一个 PR（由 Git 官方定义直接推出，属推论）。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- **清理风险**：删除 linked worktree 后应使用 `git worktree remove` 或 `prune` 维护仓库元数据；不要只依赖文件系统手工删除（推荐）。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- **共享 refs 的边界**：多个 worktree 共享部分 refs，但每个 worktree 有独立 `HEAD`；分支命名、并行分支和发布操作仍需遵循团队 Git 流程。[Git：git-worktree](https://git-scm.com/docs/git-worktree.html)
- **依赖管理缺口**：跨仓库依赖不能只靠父文件夹命名解决；应通过包版本、API 契约、发布清单或其他明确机制管理（推荐/推论）。

## 8. 仍需结合团队情况决定的事项

以下事项没有一个能由 Git 官方文档替团队给出普适答案：

- 前端和后端是否需要跨项目原子提交；若需要，monorepo 的收益是否超过单仓库治理成本。
- 团队是否需要对前端、后端、基础设施或客户代码实施不同的仓库权限；GitHub 仓库本身支持权限和公开/私有属性，但具体拆分边界取决于组织治理。[GitHub：About repositories](https://docs.github.com/en/repositories/creating-and-managing-repositories/about-repositories?apiVersion=2022-11-28)
- 发布是否共用版本号、发布窗口和回滚单元；这是发布流程决策，不是 Git 仓库类型的必然结果。
- 共享代码是“同仓库源码依赖”“独立版本化包”“锁定外部提交”，还是“需要向上游回推”的代码；分别对应 monorepo、独立仓库、submodule 或 subtree 的不同倾向。
- CI 是否按目录触发、哪些检查是 required、路径过滤后的 skipped/pending 行为如何处理。[GitHub：Workflow syntax for GitHub Actions](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)；[GitHub：Skipping workflow runs](https://docs.github.com/actions/managing-workflow-runs/skipping-workflow-runs)
- 仓库规模是否已达到需要 sparse-checkout、部分克隆、构建缓存、拆分仓库或专门 monorepo 工具的程度；应使用真实 clone/fetch/CI 数据评估，而不是套用其他公司的规模结论。[Git：sparse-checkout](https://git-scm.com/docs/git-sparse-checkout)；[GitHub：Repository limits](https://docs.github.com/en/repositories/creating-and-managing-repositories/repository-limits)
- submodule 是否允许开发者直接在嵌套目录中改代码；如果允许，应规定分支、提交、推送和更新 superproject 指针的完整流程。[Git：git-submodule](https://git-scm.com/docs/git-submodule)
- subtree 是否需要双向同步、是否保留完整历史、是否统一使用 `--squash`；这些都会影响历史可追溯性和维护工作量。[Git：git-subtree 文档](https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc)

## 9. 一句话决策表

| 你的首要目标 | 首选 |
| --- | --- |
| 前后端频繁联动、需要一次 PR 原子改变 | Monorepo |
| 独立权限、独立发布、独立历史 | 多个独立仓库 |
| 在一个主项目中锁定外部仓库的某个提交 | Git submodule |
| 把外部项目纳入普通子目录，并保留可同步/拆分能力 | Git subtree |
| 同一仓库同时处理多个分支或修复线 | Git worktree |

这张表是本文的推荐摘要，不是 Git/GitHub 的官方优先级声明。

