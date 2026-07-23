# Crynux AI Services

Crynux AS is the backend for the Crynux AI Services. It exposes the AI capabilities of the Crynux Network as managed, project-scoped services:

* Users log in with a wallet signature and get an account.
* Users pay ERC20 tokens (such as USDT and USDC) on supported blockchain networks to the platform receiving address to purchase Credits.
* Users create projects under the account. Each project gets a private OpenAI-compatible LLM API base URL and project API keys.
* LLM API calls are forwarded to the Crynux Bridge, charged from the account Credits by token usage, and recorded for per-project usage statistics.

See [docs/requirements.md](docs/requirements.md) for the functional requirements and [docs/architecture.md](docs/architecture.md) for the technical architecture.

## Build

```shell
go build
```

Or build the Docker image:

```shell
docker build -f build/crynux_as.Dockerfile -t crynux-as .
```

## Run

1. Copy `config/config.example.yml` to `config/config.yml` and fill in the config values.
2. Put the JWT secret key in `config/secrets/jwt_secret_key.txt` and the Crynux Bridge API key in `config/secrets/bridge_api_key.txt`.
3. Start the server:

```shell
./crynux_as
```

Database migrations run automatically at startup.
