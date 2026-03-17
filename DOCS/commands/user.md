# user

## Descrição

Gestão de utilizadores e sessões. Permite criar utilizadores, fazer login/logout e verificar a sessão atual.

## Sinopse

```
rmp user [subcommand] [argumentos]
```

## Subcomandos

### create

Cria um novo utilizador.

**Uso:** `rmp user create <username> <password>` ou `rmp user register <username> <password>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `username` | Sim | Nome de utilizador |
| `password` | Sim | Palavra-passe |

**Exemplo:**
```bash
rmp user create john mypassword123
```

---

### login

Inicia sessão como um utilizador.

**Uso:** `rmp user login <username> <password>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `username` | Sim | Nome de utilizador |
| `password` | Sim | Palavra-passe |

**Output:** Informação da sessão incluindo token

**Exemplo:**
```bash
rmp user login john mypassword123
```

**Output exemplo:**
```
Logged in as: john
Session token: abc123...
Expires at: 2026-03-18 14:30:00
```

---

### logout

Termina a sessão de um utilizador.

**Uso:** `rmp user logout <token>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `token` | Sim | Token da sessão |

**Exemplo:**
```bash
rmp user logout abc123...
```

---

### whoami

Mostra informação sobre o utilizador atual.

**Uso:** `rmp user whoami <token>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `token` | Sim | Token da sessão |

**Output:** Informação do utilizador e sessão

**Exemplo:**
```bash
rmp user whoami abc123...
```

**Output exemplo:**
```
Username: john
User ID: 1
Session created: 2026-03-17 14:30:00
Session expires: 2026-03-18 14:30:00
```

## Aliases

| Comando | Alias |
|---------|-------|
| `create` | `register` |

## Notas

- As palavras-passe são armazenadas com hash seguro (bcrypt)
- Os tokens de sessão têm duração limitada
- A autenticação é opcional para operações locais
