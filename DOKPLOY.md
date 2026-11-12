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

## Atualização para a v1.1.0 (pool de proxies)

Produção roda hoje `1.0.7-clipin.1` (branch `codex/fix-whatsapp-client-outdated`). A branch `atualiza-wuzapi-v1.1.0` junta a v1.1.0 do projeto original com as funções do fork e com essa correção. Ao iniciar, a versão nova aplica no PostgreSQL as migrações 12 a 15 (`repair_webhook_use_proxy`, `add_days_to_sync_history`, `add_proxy_pool`, `add_device_jid`). Elas não têm volta automática, por isso o backup é obrigatório.

### Antes

1. Escolha um horário de pouco movimento. Os números ficam fora por um a dois minutos durante a troca do container.
2. Anote a branch e o commit atuais do serviço no Dokploy (hoje `codex/fix-whatsapp-client-outdated`, commit `b07b4d7`).
3. Faça um backup do banco e confira o tamanho do arquivo:

```bash
docker exec <container-do-db> pg_dump -U wuzapi -d wuzapi -Fc -f /tmp/wuzapi-antes-v110.dump
docker cp <container-do-db>:/tmp/wuzapi-antes-v110.dump ./wuzapi-antes-v110.dump
```

4. Confirme que `WUZAPI_GLOBAL_ENCRYPTION_KEY` está guardada fora do servidor.

### Deploy

1. No Dokploy, troque a branch do serviço para `atualiza-wuzapi-v1.1.0` (ou faça o merge dela na branch usada em produção).
2. Em **Environment**, acrescente `WUZAPI_PROXY_POOL_FALLBACK=direct`. As demais variáveis novas têm padrão no `docker-compose.yml`.
3. Faça o deploy e acompanhe os logs até aparecer `Database migration applied` para as migrações 12 a 15.

### Conferência

1. `GET /health` deve mostrar `"version":"1.1.0-clipin.1"` e o mesmo número de `logged_in_users` de antes.
2. `GET /admin/proxy-pool` com o token de admin deve responder `200` com uma lista vazia.
3. Mande uma mensagem de teste em cada projeto (ClipinAgenda, ClipinShop, ClipinAtende) e confira o webhook.

### Se der errado

1. Volte a branch do serviço para `codex/fix-whatsapp-client-outdated` e faça o deploy.
2. Se a versão antiga não subir por causa das migrações, restaure o backup:

```bash
docker cp ./wuzapi-antes-v110.dump <container-do-db>:/tmp/restaurar.dump
docker exec <container-do-db> pg_restore -U wuzapi -d wuzapi --clean --if-exists /tmp/restaurar.dump
```

### Proxies

1. Cadastre cada IP com `POST /admin/proxy-pool` (`label`, `proxy_url` em `socks5://` ou `http://`, `max_devices` de 1 a 3).
2. Uma sessão sem proxy recebe um do pool na próxima conexão (`/session/connect`). Reconecte poucos números por vez e acompanhe se continuam logados.
3. Quando todos tiverem proxy, troque `WUZAPI_PROXY_POOL_FALLBACK` para `block`.
