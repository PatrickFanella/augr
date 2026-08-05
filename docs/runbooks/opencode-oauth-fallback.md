# OpenCode OAuth fallback

Use this fallback when Augr's primary LLM is unavailable and OpenCode can
authenticate to OpenAI with a ChatGPT account. Augr does not receive an OpenAI
API key or the OAuth credential. It calls a private, password-protected
OpenCode service on the internal Compose network.

## Security boundary

- The OpenCode image and digest are pinned in `docker-compose.nuc.yml`.
- The service has no published port and no Augr repository mount. Its private
  API is on the internal backend network; a separate bridge permits only the
  outbound provider traffic required for OAuth refresh and inference.
- Its working directory is an empty, no-exec temporary filesystem.
- Sharing is disabled, agent steps are limited to one, and every permission is
  denied in both the server configuration and every Augr-created session.
- Only the OpenCode service mounts its OAuth data directory. The Augr app gets
  the service password, not the OAuth credential.

## Bootstrap authentication

1. Set a long random `OPENCODE_SERVER_PASSWORD` in the deployment `.env`.
2. Set `OPENCODE_AUTH_DIR` to a root-owned deployment directory. It defaults to
   `/var/lib/augr-opencode` on the Compose host.
3. Create the directory with mode `0700`.
4. Authenticate interactively from the Compose host:

   ```sh
   docker compose -f docker-compose.nuc.yml run --rm --entrypoint opencode opencode auth login
   ```

   Select OpenAI and then the ChatGPT Plus/Pro browser login. This stores the
   refreshable OAuth credential only in `OPENCODE_AUTH_DIR`.

5. Configure the fallback:

   ```dotenv
   LLM_FALLBACK_PROVIDER=opencode
   LLM_FALLBACK_MODEL=openai/gpt-5.4-mini
   OPENCODE_BASE_URL=http://opencode:4096
   OPENCODE_SERVER_USERNAME=opencode
   OPENCODE_MODEL=openai/gpt-5.4-mini
   ```

`LLM_FALLBACK_MODEL` is optional; when present it must use OpenCode's
`provider/model` form. Keep `openai/gpt-5.4-mini` until a larger model is shown
to improve audited decisions enough to justify its latency.

## Verify

Start the service and app, then confirm health without printing the password:

```sh
docker compose -f docker-compose.nuc.yml up -d opencode app
docker compose -f docker-compose.nuc.yml ps opencode app
docker compose -f docker-compose.nuc.yml exec -T opencode sh -ec \
  'auth=$(printf "%s:%s" "$OPENCODE_SERVER_USERNAME" "$OPENCODE_SERVER_PASSWORD" | base64 | tr -d "\n"); wget -qO- --header="Authorization: Basic $auth" http://127.0.0.1:4096/global/health'
```

Induce or reproduce a non-destructive primary-provider failure and verify that
the completion succeeds with `used_fallback=true` and model
`openai/gpt-5.4-mini`. Review logs for fallback count, latency, malformed JSON,
and primary recovery before considering the integration healthy.

## Rotate or revoke

- Rotate only `OPENCODE_SERVER_PASSWORD` to invalidate Augr-to-OpenCode access.
- Run `opencode auth logout` through the same one-off Compose command to revoke
  the stored provider login, then re-authenticate.
- To disable immediately, clear `LLM_FALLBACK_PROVIDER` and recreate the app.
  The primary provider remains unchanged.

Do not copy `auth.json` into the repository, the app container, logs, support
bundles, or backups that are not encrypted as secrets.
