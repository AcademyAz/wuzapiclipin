# Deploy no Dokploy

## Serviço

Crie um serviço do tipo **Docker Compose** com esta origem:

- Repositório: `https://github.com/AcademyAz/wuzapiclipin.git`
- Branch: `main`
- Compose path: `docker-compose.yml`

Não use o modo Docker Stack, pois a aplicação é compilada com `build`.

## Variáveis

Cadastre no menu **Environment** as variáveis de `.env.example`. Gere os segredos antes do primeiro deploy:

```bash
openssl rand -hex 32 # WUZAPI_ADMIN_TOKEN
openssl rand -hex 16 # WUZAPI_GLOBAL_ENCRYPTION_KEY (32 caracteres)
openssl rand -hex 32 # WUZAPI_GLOBAL_HMAC_KEY
openssl rand -hex 24 # DB_PASSWORD
```

Preserve `WUZAPI_GLOBAL_ENCRYPTION_KEY` nos backups. Trocar essa chave impede a leitura de dados sensíveis já criptografados.

## Domínio

Na aba **Domains**, associe o domínio ao serviço `wuzapi-server` usando a porta interna `8080` e habilite HTTPS.

O WuzAPI e o PostgreSQL não publicam portas diretamente no host. O acesso externo ao WuzAPI deve passar pelo proxy HTTPS do Dokploy, e o banco permanece restrito à rede interna do Compose.

## Persistência

Configure backup periódico do volume `db_data`. Ele contém os usuários, configurações e sessões vinculadas ao WhatsApp.

## Verificação

Após o deploy, valide:

1. `GET https://<dominio>/health`
2. Swagger em `https://<dominio>/api`
3. Dashboard em `https://<dominio>/dashboard`
4. Criação de um usuário com `POST /admin/users`
5. Conexão, reinício do Compose e reconexão da sessão

Restrinja `/api` e `/dashboard` no proxy ou firewall antes de liberar o serviço em produção.
