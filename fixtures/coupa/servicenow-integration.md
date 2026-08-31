# ServiceNow Integration

The ServiceNow integration synchronizes incidents and change requests between
our platform and the ServiceNow instance at `https://acme.service-now.com`.

## Fields sent to ServiceNow

The following fields are pushed on every incident create/update:

- `u_source_id` — internal incident UUID
- `short_description` — first line of the incident title
- `description` — full incident body (markdown-rendered to plain text)
- `severity` — mapped from our P1–P4 to ServiceNow 1–4
- `assignment_group` — mapped from team key via `snow_team_map.yaml`
- `caller_id` — email of reporter
- `u_source_url` — deep link back to our platform
- `cmdb_ci` — resolved via CI matcher on `service.slug`

## Jobs

| Job                  | Schedule (UTC) | Description                                    |
|----------------------|----------------|------------------------------------------------|
| `snow-push-incident` | every 2 min    | Pushes newly created/updated incidents         |
| `snow-pull-updates`  | every 5 min    | Pulls state changes made in ServiceNow         |
| `snow-reconcile`     | daily 02:15    | Full diff + reconcile of last 24h              |
| `snow-attachment-sync` | every 10 min | Uploads new attachments to ServiceNow records  |
