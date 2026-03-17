# roadmap

## Descrição

Gestão de roadmaps - os contentores de topo para tarefas e sprints. Cada roadmap é armazenado como uma base de dados SQLite independente em `~/.roadmaps/`.

## Sinopse

```
rmp roadmap [subcommand] [argumentos] [flags]
```

## Subcomandos

### list

Lista todos os roadmaps existentes.

**Uso:** `rmp roadmap list` ou `rmp road ls`

**Output:** JSON array de objetos roadmap

**Exemplo:**
```bash
rmp roadmap list
rmp road ls
```

**Output exemplo:**
```json
[
  {"name": "project1", "path": "~/.roadmaps/project1.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2.db", "size": 8192}
]
```

---

### create

Cria um novo roadmap.

**Uso:** `rmp roadmap create <name>` ou `rmp road new <name>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `name` | Sim | Nome do roadmap (alfanumérico, hífenes, underscores) |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| N/A | `--force` | bool | false | Sobrescrever se o roadmap já existir |

**Output:** JSON object com o nome do roadmap criado

**Exemplos:**
```bash
rmp roadmap create myproject
rmp road new myproject --force
```

**Output exemplo:**
```json
{"name": "myproject"}
```

---

### remove

Remove um roadmap permanentemente. Esta ação não pode ser desfeita.

**Uso:** `rmp roadmap remove <name>` ou `rmp road rm <name>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `name` | Sim | Nome do roadmap a remover |

**Exemplos:**
```bash
rmp roadmap remove myproject
rmp road rm oldproject
```

---

### use

Seleciona um roadmap como o padrão para comandos subsequentes. Evita repetir a flag `--roadmap` em cada comando.

**Uso:** `rmp roadmap use <name>` ou `rmp road use <name>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `name` | Sim | Nome do roadmap a selecionar |

**Exemplos:**
```bash
rmp roadmap use myproject
rmp road use myproject
```

---

### backup

Gestão de backups do roadmap.

**Uso:** `rmp roadmap backup [subcommand] [options]`

**Subcomandos:**

#### backup create

Cria um backup de um roadmap.

**Uso:** `rmp roadmap backup create <name>`

**Exemplo:**
```bash
rmp roadmap backup create myproject
```

#### backup restore

Restaura um roadmap a partir de um backup.

**Uso:** `rmp roadmap backup restore <name> <file>`

**Exemplo:**
```bash
rmp roadmap backup restore myproject myproject_20260317_120000.db
```

#### backup list

Lista os backups de um roadmap.

**Uso:** `rmp roadmap backup list <name>`

**Exemplo:**
```bash
rmp roadmap backup list myproject
```

---

### export

Exporta um roadmap para JSON.

**Uso:** `rmp roadmap export <name> [file.json] [--audit]`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `name` | Sim | Nome do roadmap a exportar |
| `file` | Não | Nome do ficheiro de saída (gerado automaticamente se omitido) |

**Flags:**
| Flag | Tipo | Padrão | Descrição |
|------|------|--------|-----------|
| `--audit` | bool | false | Incluir audit log na exportação |

**Exemplos:**
```bash
rmp roadmap export myproject
rmp roadmap export myproject export.json --audit
```

---

### import

Importa um roadmap a partir de JSON.

**Uso:** `rmp roadmap import <file.json> [new-name]`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `file` | Sim | Ficheiro JSON a importar |
| `new-name` | Não | Nome opcional para o roadmap importado |

**Exemplos:**
```bash
rmp roadmap import myproject_export.json
rmp roadmap import myproject_export.json newname
```

## Aliases

| Comando | Alias |
|---------|-------|
| `roadmap` | `road` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm`, `delete` |

## Notas

- Os roadmaps são armazenados em `~/.roadmaps/<name>.db`
- O diretório `~/.roadmaps/` tem permissões `0700` (apenas o proprietário)
- O ficheiro `.current` em `~/.roadmaps/` guarda o roadmap selecionado por `use`
