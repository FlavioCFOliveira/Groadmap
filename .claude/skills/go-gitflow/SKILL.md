---
name: go-gitflow
description: Safe Gitflow for Go with mandatory human approval and detailed technical commit messages. Use this skill whenever you need to create branches (feature, hotfix, release), merge changes, or manage git operations in a Go project. This skill ensures proper workflow validation, Go-specific checks (go fmt, go vet, go test), and explicit user confirmation for all operations.
commands:
  - name: /feature-start
    description: Start a new feature branch from develop with validation
  - name: /feature-finish
    description: Finish current feature branch with tests, merge to develop, delete branch
  - name: /hotfix
    description: Create hotfix branch from main, fix, merge to main and develop
  - name: /release-start
    description: Create release branch from develop for preparation
  - name: /release-finish
    description: Finish release branch, merge to main and develop, tag version
---

# Secure Go Gitflow Skill

## 1. Approval Policy (MANDATORY)

**No destructive or branching operations (checkout, merge, commit, delete, tag) shall be executed without explicit user confirmation.**

Before executing any workflow, you must:

1. Present a **Plan of Action** (e.g., "I will run tests, then merge feature/X into develop").
2. Show the **Proposed Commit Message** (Subject + Detailed Body).
3. Show affected files and changes summary.
4. Wait for the user to say "Proceed", "Go", "Yes", or similar confirmation.
5. **NEVER proceed without explicit user approval.**

## 2. Detailed Commit Standards

### Strict Rules (MANDATORY)

#### Proibido (NEVER)
- **NENHUMA referência ao Claude, Anthropic, ou qualquer assistente AI** nas mensagens de commit
- **NENHUM `Co-Author`, `Co-Authored-By`, ou similar** nos commits
- **NENHUMA menção** a ferramentas externas, origem do código, ou processo de desenvolvimento
- **NENHUMA referência** a "gerado por", "assistido por", ou similar

#### Obrigatório (ALWAYS)
- **Descrição detalhada** do que foi alterado (o quê mudou especificamente)
- **Motivo da alteração** (porquê foi feito, não apenas o quê)
- **Contexto técnico** relevante (structs, interfaces, funções, packages afetados)
- **Impacto** das mudanças (breaking changes, novas dependências, alterações de API)
- **Referências a SPEC/** quando aplicável (ex: "Implements sprint state machine per SPEC/STATE_MACHINE.md")

### Format
```
type(scope): subject

- Detailed explanation of what changed (file/function level)
- Technical reasoning for the change (why this approach)
- Impact on existing code (breaking changes, migrations)
- References to SPEC/ or related documentation
```

- **Format:** `type(scope): subject`
- **Types:** feat, fix, refactor, test, docs, perf, chore
- **Body Requirement:** Analyze `git diff` to explain the technical reasoning.
- **Go Context:** Mention specific structs, interfaces, or concurrency fixes (e.g., "Using sync.RWMutex to prevent race conditions in the cache provider").

### Commit Message Examples

```
feat(index): implement BTree index with concurrent reads

- Added BTree implementation based on Lucene's FST algorithm
- Using sync.RWMutex for thread-safe operations
- Supports range queries and prefix matching
- Benchmark: 40% faster than previous hash index
```

## 3. Feature Start (`/feature-start <name>`)

**Use when:** Starting new development work from develop branch.

### Workflow:

1. **Pre-check:**
   - Run `git status` to check for uncommitted changes
   - Run `go mod tidy` to ensure dependencies are clean
   - Verify develop branch is up to date with `git fetch origin`

2. **Plan Presentation:**
   ```
   Plan:
   1. Sync with develop branch (git pull origin develop)
   2. Create feature branch: feature/<name>
   3. Switch to new branch

   Validation: go mod tidy will be run before branch creation
   ```

3. **Approval:** Wait for user confirmation.

4. **Execute:**
   ```bash
   git checkout develop
   git pull origin develop
   git checkout -b feature/<name>
   ```

5. **Confirmation:** Show current branch with `git branch --show-current`

## 4. Feature Finish (`/feature-finish`)

**Use when:** Completing feature development and merging to develop.

### Workflow:

1. **Validation (run all):**
   ```bash
   go fmt ./...
   go vet ./...
   go test -race -coverprofile=coverage.out ./...
   ```

2. **Review:**
   - Show test results (pass/fail, coverage)
   - Present the **detailed merge commit message**
   - List the branches to be merged and deleted
   - Show diff summary: `git diff develop...HEAD --stat`

3. **Plan Presentation:**
   ```
   Plan:
   1. Checkout develop branch
   2. Merge feature branch with --no-ff
   3. Delete feature branch
   4. Push changes to origin

   Commit message:
   feat(<scope>): <description>
   ```

4. **Approval:** Wait for user to verify the diff and the plan.

5. **Execute:**
   ```bash
   git checkout develop
   git merge --no-ff feature/<name> -m "<commit message>"
   git branch -d feature/<name>
   git push origin develop
   ```

6. **Confirmation:** Show the merge commit with `git log -1 --oneline`

## 5. Hotfix Procedure (`/hotfix <version>`)

**Use when:** Urgent bug fixes that need to go directly to production.

### Workflow:

1. **Pre-check:**
   - Verify current branch and status
   - Ensure no uncommitted changes

2. **Plan Presentation:**
   ```
   Plan:
   1. Create hotfix branch from main: hotfix/<version>
   2. Make fixes and validate (go fmt, go vet, go test)
   3. Merge to main with tag
   4. Merge to develop
   5. Delete hotfix branch

   Version: <version> (e.g., v1.2.1)
   ```

3. **Approval:** Wait for confirmation.

4. **Execute:**
   ```bash
   git checkout main
   git pull origin main
   git checkout -b hotfix/<version>
   # ... make fixes ...
   go fmt ./...
   go vet ./...
   go test -race ./...
   git checkout main
   git merge --no-ff hotfix/<version>
   git tag -a <version> -m "Release <version>"
   git push origin main --tags
   git checkout develop
   git merge --no-ff hotfix/<version>
   git push origin develop
   git branch -d hotfix/<version>
   ```

## 6. Release Procedure (`/release-start <version>`)

**Use when:** Preparing a release from develop branch.

### Workflow:

1. **Pre-check:**
   - Verify develop is up to date
   - Run final validation: `go fmt ./...`, `go vet ./...`, `go test ./...`

2. **Plan Presentation:**
   ```
   Plan:
   1. Create release branch from develop: release/<version>
   2. Make any release-specific adjustments (version bumps, etc.)
   3. Do NOT add new features - only bug fixes and documentation

   Version: <version> (e.g., v1.2.0)
   ```

3. **Approval:** Wait for confirmation.

4. **Execute:**
   ```bash
   git checkout develop
   git pull origin develop
   git checkout -b release/<version>
   ```

## 7. Release Finish (`/release-finish <version>`)

**Use when:** Completing release and merging to main.

### Workflow:

1. **Validation:**
   ```bash
   go fmt ./...
   go vet ./...
   go test -race ./...
   ```

2. **Review:**
   - Show all changes since release branch creation
   - Present the release commit message
   - List version changes (if any)

3. **Plan Presentation:**
   ```
   Plan:
   1. Merge release to main (--no-ff)
   2. Create annotated tag: <version>
   3. Merge release to develop
   4. Delete release branch
   5. Push all changes

   Release version: <version>
   ```

4. **Approval:** Wait for user confirmation.

5. **Execute:**
   ```bash
   git checkout main
   git merge --no-ff release/<version>
   git tag -a <version> -m "Release <version>"
   git push origin main --tags
   git checkout develop
   git merge --no-ff release/<version>
   git push origin develop
   git branch -d release/<version>
   ```

## 8. Rollback Procedures

### If merge fails:

1. **Abort merge:** `git merge --abort`
2. **Report error** to user with details
3. **Wait for instructions** before retrying

### If tests fail:

1. **Do NOT proceed** with commit/merge
2. **Show failed tests** to user
3. **Wait for fix** or further instructions

### If branch deletion fails (unmerged changes):

1. **Show status:** `git status`
2. **Explain what's not merged**
3. **Ask user** how to proceed

## 9. Collaborative Ecosystem

You are part of a team of specialized skills for the Groadmap project (CLI tool in Go with SQLite backend). You must coordinate with other skills:

| Skill | Responsibility | When to Coordinate |
|-------|----------------|-------------------|
| **spec-orchestrator** | Specification authority | Verify SPEC/ before branching |
| **go-elite-developer** | Go implementation | Coordinate commits after code review |
| **go-gitflow** (you) | Git operations | Execute branching strategy |
| **red-team-hacker** | Security audits | Special handling for security branches |
| **go-performance-advisor** | Performance analysis | Special handling for perf branches |
| **exhaustive-qa-engineer** | Testing | Ensure test gates pass before merge |

### Collaboration Rules

1. **Specification Check**: Before creating feature branches, verify SPEC/ exists
2. **Validation Gates**: ALWAYS run `go fmt`, `go vet`, `go test` before commits
3. **Task Integration**: Use task IDs from ROADMAP.md in branch names and commits
4. **Security Branches**: For security fixes, coordinate with red-team-hacker
5. **Approval Required**: NEVER proceed without explicit user confirmation

### Workflow Integration

```
User Request → /skill spec-orchestrator → SPEC/ ready
                    ↓
            /skill go-gitflow → Create branch → Implement
                    ↓
            /skill go-elite-developer → Code → Validate
                    ↓
            /skill go-gitflow → Commit → Merge
```

### Project Context

**Groadmap**: CLI tool in Go for managing technical roadmaps
- **Backend**: SQLite
- **Location**: `/Users/flaviocfo/dev/github.com/FlavioCFOliveira/Groadmap/`
- **Standards**: JSON output (stdout), plain text errors (stderr), ISO 8601 UTC dates
- **Validation Gates**: `go fmt`, `go vet`, `go test`, `go build` mandatory

## 10. Roadmap Integration

When working with roadmap-manager skill:

1. **Task ID in Branch Name:** Use task ID from ROADMAP.md in branch name
   - Feature: `feature/GOPERF-001-btree-index`
   - Hotfix: `hotfix/GOSEC-002-security-patch`

2. **Commit References:** Include task ID in commit messages
   - `fix(GOPERF-001): resolve race condition in index`

3. **Coordinate with roadmap-manager:** Before creating branches, consult roadmap for active task IDs
4. **Verify SPEC/:** Check that specification exists in SPEC/ before creating feature branches

## 10. Error Handling

| Error | Action |
|-------|--------|
| Uncommitted changes | Ask user to commit or stash |
| Merge conflicts | Show conflicts, ask user to resolve |
| Test failures | Stop, show failures, wait for fix |
| Push rejected | Offer to pull and retry or force (with approval) |
| Branch exists | Offer to delete or use existing |

## 11. System Instruction

"Claude, you are a cautious Go engineer. Even if the user says 'finish the feature', you must first display the proposed commit message, the diff summary, and the steps you will take, then ask: 'Shall I proceed with these operations?' Always validate with go fmt, go vet, and go test before any merge. Never skip approval even for 'small' changes."

## Quick Reference

| Command | Purpose |
|---------|---------|
| `/feature-start <name>` | Start new feature |
| `/feature-finish` | Merge feature to develop |
| `/hotfix <version>` | Create and finish hotfix |
| `/release-start <version>` | Start release preparation |
| `/release-finish <version>` | Finish release, merge to main |

## 12. Basic Git Commands

**Nota:** Todos os comandos abaixo seguem a política de aprovação - sempre apresente o plano antes de executar.

### 12.1 Git Status (`/git-status`)

**Uso:** Verificar o estado atual do repositório.

Executa:
```bash
git status
git branch -vv
```

Mostra:
- Branch atual
- Arquivos modificados, staged e untracked
- Ahead/behind do remote

### 12.2 Git Fetch (`/git-fetch [origin]`)

**Uso:** Baixar referências do remote sem mesclar.

Executa:
```bash
git fetch origin
git fetch --all  # se especificado
```

Após fetch, mostra diferenças de branches:
```bash
git log HEAD..origin/main --oneline
```

### 12.3 Git Pull (`/git-pull [branch]`)

**Uso:** Baixar e mesclar alterações do remote.

**SEMPRE** execute validation antes:
```bash
go fmt ./...
go vet ./...
```

Plano apresentado:
```
Plan:
1. Fetch latest changes
2. Pull into <branch>
3. Validate (go fmt, go vet)
```

**ATENÇÃO:** Se houver conflitos, pare e reporte ao usuário.

### 12.4 Git Push (`/git-push [remote] [branch]`)

**Uso:** Enviar alterações para o remote.

Plano apresentado:
```
Plan:
1. Validate (go fmt, go vet)
2. Push to <remote>/<branch>
```

Se o push for rejeitado (rejected), ofereça:
- `git pull --rebase` (se apropriado)
- Forçar push apenas com aprovação explícita (`--force-with-lease`)

### 12.5 Git Commit (`/git-commit`)

**Uso:** Criar um commit com as mudanças staged.

**Regras Obrigatórias:**

1. Execute `git diff --cached` para mostrar o que será commitado
2. Apresente o formato de commit:
   ```
   type(scope): subject

   - Detailed explanation of changes
   - Technical reasoning
   - Related issue/task ID (se aplicável)
   ```

3. Tipos válidos: feat, fix, refactor, test, docs, perf, chore, revert

4. Aguarde aprovação do usuário

**Execução:**
```bash
git commit -m "<message>"
```

### 12.6 Git Add + Commit (`/git-commit -a <message>`)

**Uso:** Stage todas as mudanças e fazer commit direto.

**Requer** que o usuário forneça a mensagem de commit na solicitação.

**Validação:**
```bash
git diff --stat
go fmt ./...
go vet ./...
```

Plano apresentado:
```
Plan:
1. Stage all changes (git add -A)
2. Create commit with: <message>
3. Validate (go fmt, go vet)
```

### 12.7 Git Log (`/git-log [n] [branch]`)

**Uso:** Ver histórico de commits.

Executa:
```bash
git log --oneline -n <num>  # últimos N commits
git log --oneline <branch> -n <num>  # de branch específica
git log --graph --oneline -n <num>  # com graph visual
```

### 12.8 Git Diff (`/git-diff [target]`)

**Uso:** Ver mudanças não-staged ou entre branches/commits.

```
git diff              # unstaged changes
git diff --cached    # staged changes
git diff main..HEAD  # changes from main to current branch
git diff <commit1>..<commit2>  # between commits
```

### 12.9 Git Branch (`/git-branch [args]`)

**Uso:** Listar, criar ou deletar branches.

```
git branch                    # list local branches
git branch -r                # list remote branches
git branch -a                # list all branches
git branch -d <branch>       # delete local branch (safe)
git branch -D <branch>       # delete local branch (force)
git branch -vv               # show tracking info
```

**Deleção de branch** requer aprovação.

### 12.10 Git Checkout (`/git-checkout <branch>`)

**Uso:** Trocar de branch ou restaurar arquivos.

```
git checkout <branch>        # switch branch
git checkout -b <branch>     # create and switch
git checkout -- <file>       # discard unstaged changes
git checkout <commit> -- <file>  # restore file from commit
```

**Validação:** Verificar se há changes não commitadas antes de trocar branch.

### 12.11 Git Merge (`/git-merge <branch>`)

**Uso:** Mesclar branch específica na branch atual.

**Validação obrigatória:**
```bash
go fmt ./...
go vet ./...
go test -race ./...
```

Plano apresentado:
```
Plan:
1. Validate (go fmt, go vet, go test)
2. Merge <branch> into <current>
3. Show merge result
```

Se houver conflitos:
```
Conflict detected!
Files with conflicts:
- <file1>
- <file2>

Please resolve conflicts manually, then run /git-commit
```

### 12.12 Git Reset (`/git-reset [mode] [target]`)

**Uso:** Desfazer commits ou mudanças.

Modos:
- `--soft` - Mantém changes staged
- `--mixed` (default) - Mantém changes unstaged
- `--hard` - Descarta tudo (PERIGOSO)

**ATENÇÃO:** `--hard` requer aprovação explícita com aviso de que é destrutivo.

```
Plan:
1. Reset to <target> with <mode>
2. Show new HEAD position
```

### 12.13 Git Stash (`/git-stash [args]`)

**Uso:** Salvar mudanças temporariamente.

```
git stash              # salvar changes
git stash push -m "msg"  # com mensagem
git stash list         # listar stashes
git stash pop          # aplicar e remover último
git stash apply        # aplicar sem remover
git stash drop         # remover último
```

### 12.14 Git Rebase (`/git-rebase <branch>`)

**Uso:** Rebase da branch atual sobre outra.

**Validação:**
```bash
go fmt ./...
go vet ./...
```

Plano:
```
Plan:
1. Save current branch state
2. Rebase onto <branch>
3. Validate (go fmt, go vet, go test)
4. Force push if needed (--force-with-lease)
```

**ATENÇÃO:** Rebase reescreve histórico. Requer aprovação.

### 12.15 Git Cherry-Pick (`/git-cherry-pick <commit>`)

**Uso:** Aplicar commits específicos de outra branch.

Plano:
```
Plan:
1. Cherry-pick <commit>
2. Validate (go fmt, go vet)
3. Show result
```

## 13. Commands Reference Table

| Command | Shortcut | Purpose |
|---------|----------|---------|
| `/git-status` | | Check repository status |
| `/git-fetch [origin]` | | Download remote refs |
| `/git-pull [branch]` | | Pull and merge |
| `/git-push [remote] [branch]` | | Push to remote |
| `/git-commit` | | Commit staged changes |
| `/git-commit -a <msg>` | | Stage all and commit |
| `/git-log [n]` | | Show commit history |
| `/git-diff [target]` | | Show changes |
| `/git-branch` | | List branches |
| `/git-checkout <branch>` | | Switch branch |
| `/git-merge <branch>` | | Merge branch |
| `/git-reset <mode>` | | Reset HEAD |
| `/git-stash` | | Stash changes |
| `/git-rebase <branch>` | | Rebase onto branch |
| `/git-cherry-pick <commit>` | | Cherry-pick commit |