---
name: doc-manager
description: |
  Gestão automática de documentação para projetos CLI em Go.

  USE esta skill quando o utilizador pedir para:
  - "gerar documentação", "criar docs", "atualizar documentação"
  - "sincronizar docs com o código"
  - "documentar comandos CLI"
  - Qualquer pedido relacionado com documentação do projeto, README, ou docs de comandos

  Esta skill é EXCLUSIVA para projetos Go com estrutura CLI (comandos/subcomandos).
  NÃO use para documentação de APIs REST, bibliotecas, ou projetos não-CLI.
---

# doc-manager

Skill para gerir automaticamente a documentação de projetos CLI em Go.

## Objetivo

Manter a documentação do projeto sincronizada com o código fonte e as especificações técnicas, gerando automaticamente:

1. **README.md** na raiz do projeto com índice de comandos
2. **Ficheiros markdown** por comando em `./DOCS/commands/`

## Quando Usar

- Ao adicionar novos comandos/subcomandos
- Quando alterar argumentos, flags ou comportamento de comandos
- Para manter documentação consistente com o código
- Para gerar documentação inicial de um projeto CLI

## Processo de Execução

### Passo 1: Análise do Estado Atual

1. Verificar existência da pasta `./DOCS/` e `./DOCS/commands/`
2. Ler `README.md` existente (se houver)
3. Identificar documentação de comandos existente
4. Comparar com estrutura atual do código

### Passo 2: Análise da Fonte de Verdade

Extrair informação de:

1. **Código fonte** (`internal/commands/`):
   - Nome dos comandos
   - Subcomandos disponíveis
   - Argumentos (posicionais)
   - Flags (curtas `-f` e longas `--flag`)
   - Descrições das flags
   - Valores padrão
   - Comandos obrigatórios vs opcionais

2. **Especificação técnica** (`SPEC/COMMANDS.md`):
   - Hierarquia de comandos
   - Aliases
   - Descrições formais
   - Exemplos de uso

### Passo 3: Geração de Documentação

#### README.md (raiz do projeto)

Estrutura obrigatória:
```markdown
# [Nome do Projeto]

[Breve descrição do projeto]

## Comandos Disponíveis

| Comando | Descrição | Documentação |
|---------|-----------|--------------|
| `[comando]` | [Descrição curta] | [DOCS/commands/comando.md](DOCS/commands/comando.md) |

## Instalação

[Instruções de instalação]

## Uso Rápido

[Exemplo básico de uso]
```

#### Ficheiros por Comando (`DOCS/commands/{comando}.md`)

Template por comando:

```markdown
# [Nome do Comando]

## Descrição

[Descrição completa extraída do SPEC/código]

## Sinopse

```
[comando] [subcomando] [argumentos] [flags]
```

## Subcomandos

### [subcomando-1]

**Uso:** `[comando] [subcomando-1] [args] [flags]`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `arg1` | Sim | Descrição |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-f` | `--flag` | string | "" | Descrição |

**Exemplos:**
```bash
# Exemplo 1
[comando] [subcomando-1] --flag valor

# Exemplo 2
[comando] [subcomando-1] arg1 -v
```

### [subcomando-2]
...

## Aliases

- `[alias1]` → `[comando]`

## Notas

[Notas adicionais relevantes]
```

### Passo 4: Gestão de Conflitos

**ANTES de escrever qualquer ficheiro:**

1. Verificar se ficheiro já existe
2. Se existir, mostrar diff entre versão existente e nova
3. **PERGUNTAR ao utilizador**:
   - "Sobrescrever [ficheiro]? (s/n)"
   - "Mostrar diff completo antes? (s/n)"
4. Só escrever após confirmação explícita

### Passo 5: Validação

Após geração:

1. Verificar todos os links no README.md apontam para ficheiros existentes
2. Verificar formatação markdown válida
3. Confirmar estrutura de diretórios está correta

## Formato de Output

Para cada operação, reportar:

```
=== Documentação Gerada ===

✓ README.md atualizado
  - 5 comandos indexados
  - Todos os links verificados

✓ DOCS/commands/roadmap.md criado
  - 4 subcomandos documentados
  - 12 flags catalogadas

⚠ DOCS/commands/task.md modificado
  - Aguardando confirmação do utilizador

=== Resumo ===
Criados: 2 | Atualizados: 1 | Inalterados: 2
```

## Extração de Informação

### Do Código Go

Procurar em `internal/commands/*.go`:

```go
// Comando principal
var [Nome]Cmd = &cobra.Command{
    Use:   "[uso]",
    Short: "[descrição curta]",
    Long:  "[descrição longa]",
}

// Subcomando
var [Nome]SubCmd = &cobra.Command{...}

// Flags
[Nome]Cmd.Flags().StringP("[long]", "[short]", "[default]", "[description]")
[Nome]Cmd.Flags().BoolP("[long]", "[short]", [default], "[description]")
```

### Do SPEC

Ler `SPEC/COMMANDS.md` e `SPEC/HELP_EXAMPLES.md` para:
- Descrições formais
- Exemplos de uso
- Aliases documentados

## Convenções

1. **Nomes de ficheiros**: lowercase, sem espaços, `.md`
   - Ex: `roadmap.md`, `task-create.md`

2. **Links relativos**: usar paths relativos à raiz
   - `[docs](DOCS/commands/roadmap.md)`

3. **Tabelas**: usar formato GitHub-flavored markdown

4. **Exemplos de código**: sempre com linguagem especificada
   - ` ```bash `, ` ```go `

5. **Datas**: não incluir datas de geração (documentação é "timeless")

## Limitações

- Apenas documenta comandos CLI (não APIs, bibliotecas, etc.)
- Requer estrutura Cobra ou similar para extração automática
- Exemplos complexos podem requerer input manual

## Tratamento de Erros

- Se `SPEC/` não existir: usar apenas código como fonte
- Se comando não tiver descrição: usar placeholder `[descrição pendente]`
- Se flag não tiver documentação: extrair do nome + tipo
