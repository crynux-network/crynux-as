# Crynux AS MCP

## Scope

Crynux AS exposes Model Context Protocol (MCP) access to the project Raw Task API for LLM (`task_type` `1`), Image (`task_type` `0`), and Video (`task_type` `3`).

There are two delivery forms:

* **Remote MCP**: Streamable HTTP hosted by Crynux AS at `/api/<endpoint_token>/mcp`.
* **Local MCP package**: an npm stdio package and a Claude Desktop `.mcpb` wrapper that call the same Raw Task HTTP API from the operator machine.

Both forms MUST use the same project authentication as Raw Task: `Authorization: Bearer <token>` where `<token>` is the project API key or a valid owner JWT, and the path `endpoint_token` MUST identify the same active project.

## Public Result Download Links

Image and Video Raw Task jobs MUST receive one `result_download_hash` at create time. Clients MUST build download URLs from the Crynux AS public origin and that hash:

* Image: `{as_public_origin}/api/result/{result_download_hash}/images/{index}`
* Video: `{as_public_origin}/api/result/{result_download_hash}/video`

`{index}` MUST be the zero-based image index for multi-image jobs. One job uses one hash for every image under that job.

These routes MUST NOT require `Authorization`. Unknown hash, non-Image path for an Image hash, non-Video path for a Video hash, job not successful, missing result file, and retention-cleaned jobs MUST return HTTP 404.

LLM jobs MUST NOT receive `result_download_hash`. Authenticated Raw Task result routes under `/api/<endpoint_token>/v1/inference_tasks/...` remain unchanged.

Result files remain under `llm.task_result_dir` and are removed with the job by `llm.job_retention_days` cleanup. After cleanup, public hash downloads MUST return HTTP 404.

## Remote MCP

### Endpoint

`POST`, `GET`, and `DELETE` on `/api/<endpoint_token>/mcp` MUST serve Streamable HTTP MCP for that project. The same `ProjectAuthMiddleware` rules as Raw Task MUST apply before MCP dispatch.

### Tools

Remote MCP MUST provide:

| Tool | Behavior |
|------|----------|
| `list_models` | List models. Optional catalog filter: chat, image, video, or merged. |
| `create_task` | Create one Raw Task (`task_type` `0`, `1`, or `3`). |
| `batch_create_tasks` | Batch create (1–100 items). |
| `get_task` | Get one task status view. |
| `batch_get_task_status` | Batch status (1–100 IDs). |
| `abort_task` | Abort one task. |
| `batch_abort_tasks` | Batch abort (1–100 IDs). |
| `get_task_llm` | Return the LLM result JSON text for a successful LLM task. |

Remote MCP MUST NOT provide any Image or Video `download_*` or `save_*` tool, including batch download or save.

When an Image or Video task succeeds, tool responses and guides MUST instruct the client to present the public download URL built from `result_download_hash`. Guides MUST state that bulk local save requires the local MCP package.

### Resources and prompts

Remote MCP MUST expose resources and prompts that document Raw Task usage, including cost-speed-quality tradeoffs and LLM result retrieval with `get_task_llm`.

### Batch scope

Remote MCP MUST support batch create, status, and abort for LLM, Image, and Video. Remote MCP MUST support `get_task_llm` per task. Remote MCP MUST NOT provide a batch Image/Video download or save tool.

## Local MCP Package

### Artifacts

* **`mcp/stdio`**: npm package that implements all local MCP tools over Raw Task HTTP. It is published to the npm registry.
* **`mcp/mcpb`**: Claude Desktop `.mcpb` wrapper. It MUST depend on a pinned published version of the stdio package, run `npm install`, then `mcpb pack`. It MUST NOT vendor or copy stdio source by file path.

stdio and mcpb release versions MUST match. A single manual GitHub Action MUST publish them in this order for one `version` input: npm publish stdio, update mcpb dependency and pack, write `public/mcp/crynux-as-mcp-<version>.mcpb`, update WebUI download/config references, then commit and push.

### Tools

Local MCP MUST provide every Remote MCP tool listed above, calling the project Raw Task HTTP API.

Local MCP MUST additionally provide:

| Tool | Behavior |
|------|----------|
| `save_task_llm` | Download LLM result JSON/text to a local file; return the path. Supports one task or a batch of task IDs. MUST NOT put the full body into the tool result by default. |
| `save_task_image` | Download Image PNG(s) to a local directory; return paths. Supports one task or a batch. |
| `save_task_video` | Download Video MP4 to a local directory; return the path. Supports one task or a batch. |

Save tools MUST write under `output_dir` when provided, otherwise the package default download directory from user config. Image and Video save MAY use the authenticated Raw result routes or the public hash URLs.

### Configuration

Local package user config MUST include:

* `base_url`: Crynux AS public origin (no trailing path required beyond the origin used to build `/api/<endpoint_token>/v1`).
* `api_key`: project API key.
* `endpoint_token`: project endpoint token.
* `download_dir` (optional): default directory for save tools.

## Division of Use

Zero-install clients (Claude Connectors, Cursor URL headers) MUST use Remote MCP and open or share public Image/Video download links.

Operators who need bulk or automated local files MUST install the local MCP package and use the `save_task_*` tools.
