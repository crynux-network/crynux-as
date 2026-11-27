# Crynux AS MCP

## Scope

Crynux AS exposes Model Context Protocol (MCP) access to the project Raw Task API for LLM (`task_type` `1`), Image (`task_type` `0`), and Video (`task_type` `3`).

There are two delivery forms:

* **Remote MCP**: Streamable HTTP hosted by Crynux AS at `/api/<endpoint_token>/mcp`.
* **Local MCP package**: an npm stdio package and a Claude Desktop `.mcpb` wrapper that call the same Raw Task HTTP API from the operator machine.

Both forms MUST use the same project authentication as Raw Task: `Authorization: Bearer <token>` where `<token>` is the project API key or a valid owner JWT, and the path `endpoint_token` MUST identify the same active project.

## Public Result Download Links

Image and Video Raw Task jobs MUST receive one `result_download_hash` at create time. When a task view reports `status` `success`, AS MUST include ready-to-use public download URLs on that view, built from configured `http.public_origin` ([configuration.md](./configuration.md)):

* Image: `result_download_urls` (absolute URL array, one entry per image index)
* Video: `result_download_url` (absolute MP4 URL)

MCP tools and guides MUST present those fields from get/status responses. Clients MUST NOT assemble public download URLs from the hash.

These public routes MUST NOT require `Authorization`. Unknown hash, non-Image path for an Image hash, non-Video path for a Video hash, job not successful, missing result file, and retention-cleaned jobs MUST return HTTP 404.

LLM jobs MUST NOT receive `result_download_hash` or download URL fields. Authenticated Raw Task result routes under `/api/<endpoint_token>/v1/inference_tasks/...` remain unchanged.

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

When an Image or Video task succeeds, tool responses MUST include `result_download_urls` or `result_download_url`, and guides MUST instruct the client to present those URLs. Guides MUST state that bulk local save requires the local MCP package.

`create_task` and `batch_create_tasks` tool descriptions MUST instruct the client to read resource `crynux://guides/raw-task` before calling. Those descriptions MUST also state that a `model_not_ready` create result still includes `client_task_id` and that the task keeps running so the client MUST continue with `get_task` / batch status.

### Resources and prompts

Remote MCP and Local MCP MUST expose the same resources and prompts. Guide markdown source of truth MUST live under `api/mcp/guides/` in this repository. After changing those files, operators MUST copy the same files into `crynux-as-webui/mcp/stdio/guides/` so the published local package stays aligned. The local package MUST NOT fetch guide text from the Remote MCP HTTP API at runtime.

Resources are application-controlled. MCP does not guarantee that a client injects resource bodies into the model context before tool calls. Critical workflow rules that must be visible without a resource read MUST also appear in tool descriptions. Longer reference text MUST live in resources.

Remote and Local MCP MUST expose these resources (MIME `text/markdown`):

| URI | Content |
|-----|---------|
| `crynux://guides/raw-task` | Main guide: what MCP can do; workflow and errors (including `model_not_ready` with continuing `client_task_id`); model overview; TaskArgs overview with pointers; Cost Level |
| `crynux://guides/task-args/llm` | LLM TaskArgs fields, schema link on `main`, examples |
| `crynux://guides/task-args/image` | Image TaskArgs fields, schema link on `main`, examples |
| `crynux://guides/task-args/video` | Video TaskArgs fields, schema link on `main`, examples |

The main guide MUST NOT embed full TaskArgs detail. TaskArgs detail MUST be read from the three task-args resources when needed.

Remote and Local MCP MUST expose these prompts, each with required argument `user_request`:

| Name | Behavior |
|------|----------|
| `run_llm_task` | Instruct the client to run one LLM Raw Task end-to-end and return text via `get_task_llm` |
| `run_image_task` | Instruct the client to run one Image Raw Task end-to-end and present `result_download_urls` |
| `run_video_task` | Instruct the client to run one Video Raw Task end-to-end and present `result_download_url` |

MCP guide and prompt text MUST NOT mention Provider models or Provider routing.

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

Save tools MUST require `output_dir` and MUST write files under that directory. Image and Video save MAY use the authenticated Raw result routes or the public hash URLs.

Local MCP MUST register the same resources and prompts as Remote MCP, reading bundled copies of the guide markdown shipped with the stdio package.

### Configuration

Local package user config MUST include:

* `base_url`: the project Base URL ending with `/api/<endpoint_token>/v1` (the same URL shown in Crynux AS WebUI).
* `api_key`: project API key.

## Division of Use

Zero-install clients (Claude Connectors, Cursor URL headers) MUST use Remote MCP and open or share public Image/Video download links.

Operators who need bulk or automated local files MUST install the local MCP package and use the `save_task_*` tools.
