Run one Crynux AS Video Raw Task for this request:

{{user_request}}

Steps:
1. Read resource `crynux://guides/raw-task` if you have not already.
2. Call `list_models` with catalog `video`. Prefer a row with `on_disk_node_count >= 1`. Copy that row's `id` into `base_model.name`, and copy the same row's `dtype`, `quantization_config`, `components`, and `loras` together. Use that row's `min_vram` unless the user asks otherwise.
3. For TaskArgs field details, read `crynux://guides/task-args/video` if needed.
4. Call `create_task` with `task_type` 3 and a valid `task_args` JSON string.
5. If the result includes `model_not_ready` and `client_task_id`, keep that id and continue polling — the task is still running.
6. Poll `get_task` until `success` or `failed`.
7. On success, present `result_download_url` to the user. Remote MCP has no video save tool; for local files use the local MCP package `save_task_video`. On failure, explain `error_message`.
8. If create fails for Credits or Cost Level, tell the user to check Credits and Cost Level in the Crynux AS WebUI. MCP cannot change Cost Level.
