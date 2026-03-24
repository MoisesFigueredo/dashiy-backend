# Backend Go

Backend inicial do sistema de metricas e financeiro, alinhado ao `architecture_logic.md`.

## Stack

- Go
- Fiber v2
- Asynq
- SQLC
- golang-migrate
- PostgreSQL
- Redis

## Estrutura

- `cmd/api`: servidor HTTP
- `cmd/worker`: workers Asynq
- `cmd/migrate`: runner de migrations
- `db/migrations`: schema versionado
- `db/query`: queries do SQLC
- `internal/webhooks`: intake, normalizacao inicial e processamento
- `internal/commissions`: aplicacao provisional das comissoes

## Fluxo inicial entregue

1. `POST /api/v1/webhooks/:platform/:companyToken`
2. Persiste o raw webhook em `webhook_deliveries`
3. Enfileira `webhook:process` na fila `critical`
4. Worker normaliza com fallback generico
5. Tenta atribuir por `utm_content` + parse do nome do anuncio
6. Salva/atualiza `transactions`
7. Gera `commission_entries` provisionais com a menor faixa da regra

## Endpoints principais

- `GET /api/v1/health`
- `POST /api/v1/webhooks/:platform/:companyToken`
- `GET /api/v1/niches`
- `GET /api/v1/offers`
- `GET /api/v1/ads`
- `GET /api/v1/transactions`
- `GET /api/v1/auth/bootstrap-status`
- `POST /api/v1/auth/system/setup`
- `POST /api/v1/auth/system/login`
- `POST /api/v1/auth/company/login`
- `GET /api/v1/auth/me`
- `GET /api/v1/system/companies`
- `POST /api/v1/system/companies`
- `GET /api/v1/system/companies/:companyID/users`
- `POST /api/v1/system/companies/:companyID/users`

## Autenticacao

Agora existem dois tipos de sessao:

- `system`: acesso administrativo para criar empresas e usuarios
- `company`: acesso do admin da empresa para entrar no app da propria companhia

Fluxo de bootstrap:

1. rode as migrations
2. abra o frontend
3. se ainda nao existir `system_users`, o frontend chama `POST /api/v1/auth/system/setup`
4. depois disso, o login do `system` passa a usar `POST /api/v1/auth/system/login`
5. o admin de empresa usa `POST /api/v1/auth/company/login`

Os endpoints autenticados aceitam `Authorization: Bearer <token>`.

Os endpoints de dados da empresa ainda aceitam os headers temporarios abaixo como fallback de compatibilidade:

- `X-Company-ID`
- `X-Niche-ID` opcional
- `X-User-ID` opcional
- `X-User-Role` opcional

## Comandos

```powershell
go run ./cmd/migrate -direction up
go run ./cmd/api
go run ./cmd/worker
```

Se preferir, o Redis pode ser configurado com uma URL unica:

```powershell
$env:REDIS_URL='redis://default:password@host:6379'
```

Se `REDIS_URL` estiver preenchida, ela tem prioridade sobre `REDIS_ADDR`, `REDIS_PASSWORD` e `REDIS_DB`.

## SQLC

Como o Go local esta em `windows/386`, para gerar o SQLC sem erro interno use:

```powershell
$env:GOARCH='amd64'
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate
```

## Observacoes

- Os normalizers por plataforma ainda estao em modo fallback generico porque voce ainda nao tem os payloads reais.
- A selecao de comissao usa o `percentage_min` como valor provisional ate o fechamento mensal.
- A base de calculo de regras `profit` esta usando faturamento bruto por enquanto, para nao travar o inicio da API antes da camada completa de agregacao de lucro.
- Para o fluxo novo de login funcionar, mantenha um `JWT_SECRET` forte configurado no `.env`.
